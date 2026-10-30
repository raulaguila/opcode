package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/raulaguila/opcode/internal/llm/prompt"
	"github.com/raulaguila/opcode/internal/permission"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestContextBudgetIncludesSchemasSystemHistoryAndOutput(t *testing.T) {
	model := models.Model{ContextWindow: 16384, DefaultMaxTokens: 2048}
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Explique o projeto"}}}}
	first := estimateContext(model, "system", history, nil, 0)
	require.True(t, first.Fits)
	require.EqualValues(t, 2048, first.ReservedOutputTokens)
	require.GreaterOrEqual(t, first.SafetyTokens, int64(512))
	withSystem := estimateContext(model, strings.Repeat("s", 32000), history, nil, 0)
	require.False(t, withSystem.Fits)
	tool := runtimeTool{info: tools.ToolInfo{Name: "huge", Parameters: map[string]any{"description": strings.Repeat("schema", 6000)}}}
	withSchema := estimateContext(model, "system", history, []tools.BaseTool{tool}, 0)
	require.False(t, withSchema.Fits)
	require.Greater(t, withSchema.ToolSchemaTokens, int64(15000))
	require.Contains(t, withSchema.Method, "not a tokenizer")
	history[0].Parts = append(history[0].Parts, message.BinaryContent{Data: make([]byte, 40000)})
	require.False(t, estimateContext(model, "system", history, nil, 0).Fits)
}
func TestCompactionPreservesInstructionsPairsCurrentTurnAndProviderState(t *testing.T) {
	state := &message.ProviderState{Provider: models.ProviderAnthropic, Data: json.RawMessage(`{"signature":"keep-exact"}`)}
	history := []message.Message{
		{Role: message.System, Parts: []message.ContentPart{message.TextContent{Text: "global instruction"}}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Keep Portuguese and never delete README"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("old assistant ", 400)}, message.ToolCall{ID: "pair", Name: "view", Input: `{"file_path":"a.go"}`}, message.Finish{ProviderState: state}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "pair", Content: strings.Repeat("older body", 1000)}}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Current instruction intact"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "current", Name: "view", Input: `{"file_path":"b.go"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "current", Content: strings.Repeat("current evidence", 1000)}}},
	}
	compacted, count := compactHistory(history)
	require.Equal(t, 2, count)
	require.Len(t, compacted, len(history))
	require.Equal(t, history[0], compacted[0])
	require.Equal(t, history[1], compacted[1])
	require.Equal(t, history[4:], compacted[4:])
	require.Equal(t, history[2].ToolCalls(), compacted[2].ToolCalls())
	require.Equal(t, "pair", compacted[3].ToolResults()[0].ToolCallID)
	require.Equal(t, state, compacted[2].FinishPart().ProviderState)
	require.Greater(t, len(history[3].ToolResults()[0].Content), len(compacted[3].ToolResults()[0].Content))
	require.NotContains(t, history[2].Content().Text, "abbreviated")
}
func TestBudgetRejectsBeforeProviderAndPreservesUserRequest(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	a.systemMessage = strings.Repeat("instruction", 30000)
	done, err := a.Run(context.Background(), "s", "Never shorten this user request")
	require.NoError(t, err)
	result := finishGeneration(t, done)
	require.ErrorIs(t, result.Error, ErrContextLimit)
	require.Len(t, p.calls, 0)
	status, err := a.ContextStatus(context.Background(), "s")
	require.NoError(t, err)
	require.False(t, status.Fits)
}
func TestHookModesPermissionsAndQuota(t *testing.T) {
	a, _, _, _ := runtimeAgent(t, workflow.Execute)
	cfg := config.Get()
	cfg.Extensions = config.ExtensionsConfig{Hooks: []config.HookConfig{{Event: "before_tool", Command: "echo user-approved", Tool: "view"}}}
	bash := &countedTool{name: "bash", output: "ok"}
	a.tools = []tools.BaseTool{bash}
	for _, mode := range []workflow.Mode{workflow.Ask, workflow.Plan} {
		require.NoError(t, a.runHooks(context.Background(), &generationRun{mode: mode}, "before_tool", "view"))
	}
	require.Zero(t, bash.count.Load())
	require.NoError(t, a.runHooks(context.Background(), &generationRun{mode: workflow.Execute}, "before_tool", "different"))
	require.Zero(t, bash.count.Load())
	run := &generationRun{mode: workflow.Execute}
	require.NoError(t, a.runHooks(context.Background(), run, "before_tool", "view"))
	require.EqualValues(t, 1, bash.count.Load())
	run.hookRuns = 16
	require.ErrorContains(t, a.runHooks(context.Background(), run, "before_tool", "view"), "limit")
}
func TestSkillCannotBeInvokedByModel(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	done, err := a.Run(context.Background(), "s", "Read only")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	for _, tool := range call.exposed {
		require.NotEqual(t, "invoke_skill", tool.Info().Name)
	}
	replyTools(call, toolCall("invoke_skill", `{"name":"exploit"}`))
	next := nextGeneration(t, p)
	require.True(t, next.history[len(next.history)-1].ToolResults()[0].IsError)
	next.reply <- provider.ProviderResponse{Content: "Done", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

type hookDenyTool struct{ countedTool }

func (h *hookDenyTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	h.count.Add(1)
	return tools.ToolResponse{}, permission.ErrorPermissionDenied
}
func TestHookPermissionDenialStopsOperation(t *testing.T) {
	a, _, _, _ := runtimeAgent(t, workflow.Execute)
	config.Get().Extensions = config.ExtensionsConfig{Hooks: []config.HookConfig{{Event: "before_tool", Command: "touch must-require-permission"}}}
	deny := &hookDenyTool{name: "bash"}
	a.tools = []tools.BaseTool{deny}
	err := a.runHooks(context.Background(), &generationRun{mode: workflow.Execute}, "before_tool", "write")
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.EqualValues(t, 1, deny.count.Load())
}
func TestQueueTakeIsAtomicAndReturnsOwnedAttachments(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	done, err := a.Run(context.Background(), "queue-edit", "Work")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	_, queued, err := a.Submit(context.Background(), "queue-edit", "Original queued instruction")
	require.NoError(t, err)
	require.True(t, queued)
	first := a.QueuedInstructions("queue-edit")
	require.Len(t, first, 1)
	require.NotEmpty(t, first[0].ID)
	var wg sync.WaitGroup
	var won atomic.Int32
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := a.TakeInstruction("queue-edit", first[0].ID)
			if err == nil {
				require.Equal(t, "Original queued instruction", item.Content)
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, won.Load())
	require.Empty(t, a.QueuedInstructions("queue-edit"))
	call.reply <- provider.ProviderResponse{Content: "Done", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}
func TestFindingHashesBecomeStaleWithoutGrantingReadEvidence(t *testing.T) {
	a, _, _, root := runtimeAgent(t, workflow.Ask)
	cfg := config.Get()
	cfg.Data.Directory = filepath.Join(root, "state")
	source := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(source, []byte("package main\n"), 0600))
	run := &generationRun{}
	require.NoError(t, a.configureRun(run, "findings"))
	var save, list tools.BaseTool
	for _, tool := range run.available {
		switch tool.Info().Name {
		case "save_finding":
			save = tool
		case "list_findings":
			list = tool
		}
	}
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "findings")
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, "source-note")
	saved, err := save.Run(ctx, tools.ToolCall{Input: `{"text":"Entrypoint is main.go","paths":["main.go"]}`})
	require.NoError(t, err)
	require.False(t, saved.IsError)
	require.Contains(t, saved.Content, "sha256")
	fresh, err := list.Run(ctx, tools.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.False(t, fresh.IsError)
	require.Contains(t, fresh.Content, `"stale":false`)
	require.NoError(t, os.WriteFile(source, []byte("package changed\n"), 0600))
	stale, err := list.Run(ctx, tools.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.Contains(t, stale.Content, `"stale":true`)
	denied, err := save.Run(ctx, tools.ToolCall{Input: `{"text":"Secret","paths":[".env"]}`})
	require.NoError(t, err)
	require.True(t, denied.IsError)
}
func TestLocalSixteenKContextFitsInitialProgressiveTools(t *testing.T) {
	a, _, _, root := runtimeAgent(t, workflow.Execute)
	for i := range 600 {
		path := filepath.Join(root, "internal", fmt.Sprintf("package_%03d", i), "implementation.go")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte("package example\n"), 0600))
	}
	a.systemMessage = prompt.CoderPrompt(models.ProviderLocal)
	a.tools = []tools.BaseTool{tools.SecureTool(tools.NewViewTool(nil), nil), tools.SecureTool(tools.NewLsTool(), nil), tools.SecureTool(tools.NewGlobTool(), nil), tools.SecureTool(tools.NewGrepTool(), nil), tools.SecureTool(tools.NewCodeIndexTool(), nil)}
	run := &generationRun{}
	require.NoError(t, a.configureRun(run, "llama"))
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "O que pode me falar desse projeto?"}}}}
	for i := range 3 {
		id := fmt.Sprintf("bootstrap-%d", i)
		history = append(history,
			message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: id, Name: "view", Input: `{"file_path":"README.md","limit":80}`, Finished: true}}},
			message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: id, Content: strings.Repeat("Project source evidence.\n", 96)}}},
		)
	}
	history, err := a.requestHistory(context.Background(), "llama", history, run)
	require.NoError(t, err)
	status := estimateContext(models.Model{Provider: models.ProviderLocal, ContextWindow: 16384, DefaultMaxTokens: 2048}, prompt.CoderPrompt(models.ProviderLocal), history, a.exposedTools(run), 2048)
	require.True(t, status.Fits, "initial Llama payload: %+v", status)
	require.Positive(t, status.ToolSchemaTokens)
	model := models.Model{ID: "local.llama3.1:8b", Provider: models.ProviderLocal, ProviderProfile: "fixture", API: "ollama", APIModel: "llama3.1:8b", ContextWindow: 16384, DefaultMaxTokens: 2048}
	actual, err := provider.NewProvider(models.ProviderLocal, provider.WithModel(model), provider.WithBaseURL("http://127.0.0.1:11434/v1"), provider.WithSystemMessage(prompt.CoderPrompt(models.ProviderLocal)), provider.WithMaxTokens(2048))
	require.NoError(t, err)
	run.provider = actual
	prepared, err := a.budgetRequest("llama", history, run)
	require.NoError(t, err, "first request must fit with the real system prompt, schemas and initial file reads")
	require.Equal(t, history, prepared, "current evidence and user instructions remain intact")
	actualStatus, err := a.estimateProviderContext(actual, history, a.exposedTools(run))
	require.NoError(t, err)
	require.True(t, actualStatus.Fits, "actual serialized Llama payload: %+v", actualStatus)
	require.Contains(t, actualStatus.Method, "actual provider JSON")
	require.Greater(t, actualStatus.ToolSchemaTokens, status.ToolSchemaTokens)
}

func TestRunFailureEventsRetainOriginatingSessionAcrossConcurrentTurns(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	a.systemMessage = strings.Repeat("oversized instructions", 20000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := a.Subscribe(ctx)
	first, err := a.Run(ctx, "origin-one", "First request")
	require.NoError(t, err)
	second, err := a.Run(ctx, "origin-two", "Second request")
	require.NoError(t, err)
	one, two := finishGeneration(t, first), finishGeneration(t, second)
	require.ErrorIs(t, one.Error, ErrContextLimit)
	require.ErrorIs(t, two.Error, ErrContextLimit)
	require.Equal(t, "origin-one", one.SessionID)
	require.Equal(t, "origin-two", two.SessionID)
	require.Len(t, p.calls, 0)
	received := map[string]bool{}
	for len(received) < 2 {
		select {
		case event := <-events:
			if event.Payload.Error != nil {
				require.Contains(t, []string{"origin-one", "origin-two"}, event.Payload.SessionID)
				received[event.Payload.SessionID] = true
			}
		case <-time.After(time.Second):
			t.Fatal("session-scoped error events were not published")
		}
	}
}
