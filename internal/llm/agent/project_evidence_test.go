package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestEncodedChoicesWaitForHumanWithoutRepairGeneration(t *testing.T) {
	for _, mode := range []workflow.Mode{workflow.Ask, workflow.Execute} {
		t.Run(string(mode), func(t *testing.T) {
			a, p, _, _ := runtimeAgent(t, mode)
			a.questions.SetInteractive(true)
			done, err := a.Run(context.Background(), "s", "me faça uma pergunta")
			require.NoError(t, err)
			replyTools(nextGeneration(t, p), toolCall("ask_user_question", `{"question":"Qual área você quer revisar?","choices":"[\"UI\",\"Contexto\"]","explanation":""}`))
			var q interaction.Question
			require.Eventually(t, func() bool { var ok bool; q, ok = a.questions.Pending("s"); return ok }, time.Second, time.Millisecond)
			require.Equal(t, []string{"UI", "Contexto"}, q.Choices)
			select {
			case <-p.calls:
				t.Fatal("model called again before human answered")
			default:
			}
			require.NoError(t, a.questions.Answer("s", q.ID, "Contexto"))
			last := nextGeneration(t, p)
			require.Contains(t, lastSent(last.history).ToolResults()[0].Content, "Contexto")
			last.reply <- provider.ProviderResponse{Content: "Vamos revisar o contexto.", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
			task, err := a.workflow.Get(context.Background(), "s")
			require.NoError(t, err)
			require.False(t, task.Changed)
		})
	}
}

func TestBootstrapReusesOnlyCurrentVisibleSourcesAndProjectsOnce(t *testing.T) {
	a, p, saved, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# UNIQUE_REPOSITORY_FACT\nRepository evidence.\n"), 0600))
	read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
	a.tools = []tools.BaseTool{read}
	for i := range 3 {
		if i == 2 {
			require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# UNIQUE_REPOSITORY_FACT\nChanged evidence.\n"), 0600))
		}
		done, err := a.Run(context.Background(), "s", "O que pode me falar do projeto atual em @README.md ?")
		require.NoError(t, err)
		call := nextGeneration(t, p)
		raw, err := json.Marshal(call.history)
		require.NoError(t, err)
		if i == 1 {
			require.EqualValues(t, 1, read.count.Load(), "unchanged evidence must not invoke view again")
			require.Equal(t, 1, countSourceBodies(call.history, "UNIQUE_REPOSITORY_FACT"), "do not duplicate source bodies in the provider request")
			history, err := saved.List(context.Background(), "s")
			require.NoError(t, err)
			require.Equal(t, 2, countSourceBodies(history, "UNIQUE_REPOSITORY_FACT"), "persisted transcript is intact")
		}
		if i == 2 {
			require.EqualValues(t, 2, read.count.Load(), "changed source requires a fresh read")
			require.Contains(t, string(raw), "Changed evidence")
		}
		call.reply <- provider.ProviderResponse{Content: "O projeto está descrito em README.md:1-2.", FinishReason: message.FinishReasonEndTurn}
		require.NoError(t, finishGeneration(t, done).Error)
	}
}

func countSourceBodies(history []message.Message, needle string) int {
	count := 0
	for _, msg := range history {
		for _, r := range msg.ToolResults() {
			count += strings.Count(r.Content, needle)
		}
	}
	return count
}

func TestProviderCannotAssignContextProvenance(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	a.tools = []tools.BaseTool{&countedTool{name: "view", output: "result"}}
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	call := toolCall("view", `{}`)
	call.Origin = "context-reused"
	replyTools(nextGeneration(t, p), call)
	last := nextGeneration(t, p)
	for _, msg := range last.history {
		for _, call := range msg.ToolCalls() {
			require.Empty(t, call.Origin)
		}
	}
	last.reply <- provider.ProviderResponse{Content: "Concluído.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestReusedSourceDoesNotSatisfyExplicitToolCall(t *testing.T) {
	run := &generationRun{requiredCalls: map[string]int{"view": 1}, startMessageID: "user"}
	history := []message.Message{
		{ID: "user", Role: message.User},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "reused", Name: "view", Origin: "context-reused"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "reused", Content: "verified source"}}},
	}
	require.NotEmpty(t, missingRequiredCalls(run, history))
	history = append(history,
		message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "actual", Name: "view"}}},
		message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "actual", Content: "fresh read"}}})
	require.Empty(t, missingRequiredCalls(run, history))
}

func TestMentionedFileIsReadWithItsImportersBeforeTheModel(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "store"), 0700))
	for path, content := range map[string]string{
		"go.mod":                  "module example.com/app\n",
		"internal/store/store.go": "package store\n\nfunc Save() {}\n",
		"main.go":                 "package main\n\nimport \"example.com/app/internal/store\"\n\nfunc main() { store.Save() }\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0600))
	}
	a.tools = []tools.BaseTool{tools.NewViewTool(nil), tools.NewGrepTool()}
	done, err := a.Run(context.Background(), "s", "o que faz @internal/store/store.go?")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	raw, _ := json.Marshal(call.history)
	require.Contains(t, string(raw), "func Save()", "the mentioned file is read without a project keyword")
	require.Contains(t, string(raw), "main.go", "its importers come along")
	call.reply <- provider.ProviderResponse{Content: "Salva dados; main.go a chama (internal/store/store.go:3).", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestSourcesReadListsSuccessfulReadsOnce(t *testing.T) {
	calls := []message.ToolCall{
		{ID: "1", Name: "view", Input: `{"file_path":"main.go"}`},
		{ID: "2", Name: "view", Input: `{"file_path":"main.go"}`},
		{ID: "3", Name: "read_lines", Input: `{"file_path":"cmd/root.go"}`},
		{ID: "4", Name: "view", Input: `{"file_path":"missing.go"}`},
		{ID: "5", Name: "grep", Input: `{"pattern":"x"}`},
	}
	results := map[string]message.ToolResult{"1": {}, "2": {}, "3": {}, "4": {IsError: true}, "5": {}}
	read := SourcesRead(calls, results)
	require.Equal(t, []string{"main.go", "cmd/root.go"}, read)
	require.Equal(t, "a, b, c +1", SourcesLabel([]string{"a", "b", "c", "d"}))
}

func TestQuestionWordsNeverPreloadSources(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Execute)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# Project\n"), 0600))
	read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
	a.tools = []tools.BaseTool{read}
	done, err := a.Run(context.Background(), "s", "analise o fluxo com o modelo deste projeto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	require.Zero(t, read.count.Load())
	call.reply <- provider.ProviderResponse{Content: "Preciso ler o código primeiro.", FinishReason: message.FinishReasonEndTurn}
	finishGeneration(t, done)
}

type citationReadTool struct {
	tools.BaseTool
	count atomic.Int32
}

func (t *citationReadTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	t.count.Add(1)
	return t.BaseTool.Run(ctx, call)
}
