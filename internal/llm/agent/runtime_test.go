package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type countedTool struct {
	name, output string
	count        atomic.Int32
}

func (t *countedTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: t.name, Parameters: map[string]any{}}
}
func (t *countedTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	t.count.Add(1)
	return tools.NewTextResponse(t.output), nil
}
func runtimeAgent(t *testing.T, mode workflow.Mode) (*agent, *controlledProvider, *memoryMessages, string) {
	a, p, m := steeringAgent(t)
	cfg := config.Get()
	old := *cfg
	root := t.TempDir()
	cfg.WorkingDir = root
	a.workflow = workflow.New(nil, root, mode)
	a.questions = interaction.NewQuestions()
	t.Cleanup(func() { _ = a.Close(); *cfg = old })
	return a, p, m, root
}
func toolNames(call generationCall) []string {
	var names []string
	for _, tool := range call.exposed {
		names = append(names, tool.Info().Name)
	}
	return names
}
func replyTools(call generationCall, calls ...message.ToolCall) {
	call.reply <- provider.ProviderResponse{ToolCalls: calls, FinishReason: message.FinishReasonToolUse}
}
func toolCall(name, input string) message.ToolCall {
	return message.ToolCall{ID: name + "-id", Name: name, Input: input, Finished: true}
}

func TestReadModesRejectCommandsEditsAndOpaqueIntegrations(t *testing.T) {
	for _, mode := range []workflow.Mode{workflow.Ask, workflow.Plan} {
		t.Run(string(mode), func(t *testing.T) {
			a, p, _, _ := runtimeAgent(t, mode)
			bash, edit, opaque := &countedTool{name: "bash"}, &countedTool{name: "edit"}, &countedTool{name: "mcp_untrusted"}
			a.tools = []tools.BaseTool{bash, edit, opaque}
			done, err := a.Run(context.Background(), "s", "Leia o código")
			require.NoError(t, err)
			first := nextGeneration(t, p)
			for _, name := range []string{"bash", "edit", "mcp_untrusted"} {
				require.NotContains(t, toolNames(first), name)
			}
			replyTools(first, toolCall("request_tools", `{"names":["bash"]}`), toolCall("bash", `{"command":"touch forbidden"}`), toolCall("edit", `{}`), toolCall("mcp_untrusted", `{}`))
			next := nextGeneration(t, p)
			results := next.history[len(next.history)-1].ToolResults()
			require.Len(t, results, 4)
			for _, result := range results {
				require.True(t, result.IsError)
			}
			next.reply <- provider.ProviderResponse{Content: "Sem alterações.", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
			require.Zero(t, bash.count.Load())
			require.Zero(t, edit.count.Load())
			require.Zero(t, opaque.count.Load())
		})
	}
}
func TestProgressiveToolsLoadForNextRequest(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	extra := &countedTool{name: "extra", output: "real result"}
	a.tools = []tools.BaseTool{extra}
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.NotContains(t, toolNames(first), "extra")
	replyTools(first, toolCall("request_tools", `{"names":["extra"]}`))
	next := nextGeneration(t, p)
	require.Contains(t, toolNames(next), "extra")
	replyTools(next, toolCall("extra", `{}`))
	last := nextGeneration(t, p)
	require.Contains(t, last.history[len(last.history)-1].ToolResults()[0].Content, "real result")
	last.reply <- provider.ProviderResponse{Content: "Feito", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	require.EqualValues(t, 1, extra.count.Load())
}
func TestProjectOverviewReceivesActualLocalEvidenceBeforeModel(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# RioSensor\nMonitora sensores de nível."), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module rio/sensor\ngo 1.26\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("NEVER_PREFETCH_THIS"), 0600))
	a.tools = []tools.BaseTool{tools.SecureTool(tools.NewViewTool(nil), permission.NewPermissionService())}
	done, err := a.Run(context.Background(), "s", "O que pode me falar do projeto atual?")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	raw, _ := json.Marshal(first.history)
	require.Contains(t, string(raw), "RioSensor")
	require.Contains(t, string(raw), "rio/sensor")
	require.NotContains(t, string(raw), "NEVER_PREFETCH_THIS")
	require.Equal(t, message.User, first.history[0].Role)
	require.Contains(t, first.history[0].Content().String(), "O que pode me falar do projeto atual?")
	first.reply <- provider.ProviderResponse{Content: "RioSensor monitora sensores (README.md:1-2).", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}
func TestRepeatedCallsStopAndSaveBlockedTask(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	repeat := &countedTool{name: "view", output: "unchanged"}
	a.tools = []tools.BaseTool{repeat}
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	for range 4 {
		call := nextGeneration(t, p)
		replyTools(call, toolCall("view", `{"x":1}`))
	}
	require.ErrorContains(t, finishGeneration(t, done).Error, "repeated")
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, "blocked", task.Status)
}
func TestStepAndResultBudgetsBoundGeneration(t *testing.T) {
	for _, kind := range []string{"steps", "results"} {
		t.Run(kind, func(t *testing.T) { testStepAndResultBudget(t, kind) })
	}
}
func TestModeCannotChangeDuringRunAndRevokesOnIdleChange(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	var revoked atomic.Int32
	a.onModeChange = func(string) { revoked.Add(1) }
	done, err := a.Run(context.Background(), "s", "Olá")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	require.ErrorIs(t, a.SetMode(context.Background(), "s", workflow.Ask), ErrSessionBusy)
	call.reply <- provider.ProviderResponse{Content: "Olá", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	require.NoError(t, a.SetMode(context.Background(), "s", workflow.Ask))
	require.EqualValues(t, 1, revoked.Load())
}

func TestActualChecksMustRunAgainAfterExternalEdit(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Execute)
	config.Get().Security.Sandbox = "none"
	config.Get().Checks = map[string]config.CheckConfig{"test": {Command: "printf validated"}}
	path := filepath.Join(root, "source.go")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	permissions := permission.NewPermissionService()
	permissions.AutoApproveSession("s")
	a.tools = []tools.BaseTool{tools.SecureTool(tools.NewChecksTool(permissions), permissions)}
	done, err := a.Run(context.Background(), "s", "Validar os arquivos")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	replyTools(first, toolCall("run_checks", `{"names":["test"]}`))
	next := nextGeneration(t, p)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Len(t, task.Validation, 1)
	require.True(t, task.Validation[0].Passed)
	require.NoError(t, os.WriteFile(path, []byte("external change"), 0600))
	next.reply <- provider.ProviderResponse{Content: "Tudo validado", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().String(), "outdated validation")
	replyTools(repair, toolCall("run_checks", `{"names":["test"]}`))
	final := nextGeneration(t, p)
	final.reply <- provider.ProviderResponse{Content: "Verificação repetida para os arquivos atuais.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	task, err = a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.EqualValues(t, 1, task.Revision)
	require.EqualValues(t, 1, task.Validation[0].Revision)
	require.True(t, task.Validation[0].Passed)
}

func TestDeniedCheckDoesNotRecordSuccess(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	config.Get().Checks = map[string]config.CheckConfig{"test": {Command: "printf must-not-run"}}
	permissions := permission.NewPermissionService()
	a.tools = []tools.BaseTool{tools.SecureTool(tools.NewChecksTool(permissions), permissions)}
	done, err := a.Run(context.Background(), "s", "Validar")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	replyTools(first, toolCall("run_checks", `{"names":["test"]}`))
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Empty(t, task.Validation)
}

func TestPlanProtocolPersistsAndResumesInExecuteMode(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Plan)
	done, err := a.Run(context.Background(), "s", "Planeje a alteração")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	replyTools(first, toolCall("update_plan", `{"objective":"Corrigir","steps":[{"title":"Implementar","status":"pending"}],"criteria":["Funciona"],"checks":[]}`), toolCall("finish_task", `{"status":"planned","summary":"Pronto para revisão"}`))
	next := nextGeneration(t, p)
	next.reply <- provider.ProviderResponse{Content: "Plano pronto para revisão.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, "planned", task.Status)
	require.NoError(t, a.SetMode(context.Background(), "s", workflow.Execute))
	done, err = a.Run(context.Background(), "s", "Continue")
	require.NoError(t, err)
	resumed := nextGeneration(t, p)
	require.Contains(t, resumed.history[len(resumed.history)-1].Content().String(), "Implementar")
	replyTools(resumed, toolCall("finish_task", `{"status":"blocked","summary":"Sem autorização para implementar"}`))
	last := nextGeneration(t, p)
	last.reply <- provider.ProviderResponse{Content: "Tarefa bloqueada.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

type interruptedProvider struct {
	provider.Provider
	started, release chan struct{}
}

func (p *interruptedProvider) StreamResponse(ctx context.Context, _ []message.Message, _ []tools.BaseTool) <-chan provider.ProviderEvent {
	out := make(chan provider.ProviderEvent, 2)
	go func() {
		defer close(out)
		call := toolCall("view", `{"file_path":"source.go"}`)
		out <- provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCall: &call}
		close(p.started)
		select {
		case <-p.release:
			out <- provider.ProviderEvent{Type: provider.EventError, Error: fmt.Errorf("connection stopped")}
		case <-ctx.Done():
		}
	}()
	return out
}
func TestInterruptedToolStreamRepairsProtocolAndKeepsAcceptedInstructions(t *testing.T) {
	a, p, m, _ := runtimeAgent(t, workflow.Execute)
	broken := &interruptedProvider{Provider: p, started: make(chan struct{}), release: make(chan struct{})}
	a.provider = broken
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	<-broken.started
	_, queued, err := a.Submit(context.Background(), "s", "Instrução recebida durante a falha")
	require.NoError(t, err)
	require.True(t, queued)
	close(broken.release)
	require.ErrorContains(t, finishGeneration(t, done).Error, "connection stopped")
	history, err := m.List(context.Background(), "s")
	require.NoError(t, err)
	require.Len(t, history, 4)
	require.Equal(t, message.Tool, history[2].Role)
	require.True(t, history[2].ToolResults()[0].IsError)
	require.Equal(t, history[1].ToolCalls()[0].ID, history[2].ToolResults()[0].ToolCallID)
	require.Contains(t, history[3].Content().String(), "Instrução recebida")
	a.provider = p
	done, err = a.Run(context.Background(), "s", "Continue")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	call.reply <- provider.ProviderResponse{Content: "Retomado", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestTurnDeadlineCancelsSilentProviderAndKeepsTask(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	config.Get().Runtime.MaxSeconds = 1
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	_ = nextGeneration(t, p)
	require.ErrorIs(t, finishGeneration(t, done).Error, context.DeadlineExceeded)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, "blocked", task.Status)
}

func testStepAndResultBudget(t *testing.T, kind string) {
	a, p, m, _ := runtimeAgent(t, workflow.Execute)
	cfg := config.Get()
	cfg.Runtime.MaxSteps = 1
	cfg.Runtime.ToolBudget = 4096
	output := "result"
	if kind == "results" {
		cfg.Runtime.MaxSteps = 10
		output = strings.Repeat("x", 8192)
	}
	a.tools = []tools.BaseTool{&countedTool{name: "view", output: output}}
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("view", `{}`))
	result := finishGeneration(t, done)
	require.Error(t, result.Error)
	if kind == "steps" {
		require.ErrorContains(t, result.Error, "step limit")
	} else {
		require.ErrorContains(t, result.Error, "budget")
		items, err := m.List(context.Background(), "s")
		require.NoError(t, err)
		for _, item := range items {
			for _, result := range item.ToolResults() {
				require.LessOrEqual(t, len(result.Content), 4096)
			}
		}
	}
}
