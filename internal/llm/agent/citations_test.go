package agent

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestExplanationRepairsMissingCitationWithoutRepeatingRead(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	read := &countedTool{name: "view", output: "def add(a, b):\n    return a - b"}
	a.tools = []tools.BaseTool{read}
	done, err := a.Run(context.Background(), "s", "Localize a causa do erro; leia counter.py e explique sem editar arquivos.")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{"file_path":"counter.py"}`))
	answer := nextGeneration(t, p)
	answer.reply <- provider.ProviderResponse{Content: "add usa subtração em vez de soma.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "counter.py")
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "Do not invent citations")
	repair.reply <- provider.ProviderResponse{Content: "Em counter.py:2, add usa a - b em vez de a + b.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Contains(t, result.Message.Content().Text, "counter.py:2")
	require.EqualValues(t, 1, read.count.Load())
}

func TestCitationRepairIsBoundedAndDoesNotFabricateReferences(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	a.tools = []tools.BaseTool{&countedTool{name: "view", output: "source"}}
	done, err := a.Run(context.Background(), "s", "Explain this code")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{"file_path":"source.go"}`))
	for i := 0; i < 2; i++ {
		nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Uncited explanation", FinishReason: message.FinishReasonEndTurn}
	}
	require.ErrorContains(t, finishGeneration(t, done).Error, "citation remains missing")
}

func TestCitationPresenceUsesOnlyCurrentSuccessfulReadReceipts(t *testing.T) {
	runtimeAgent(t, workflow.Ask)
	run := &generationRun{requiresCitation: true, startMessageID: "prior-request", turnMessageID: "current"}
	history := []message.Message{
		{ID: "prior-request", Role: message.User},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "old", Name: "view", Input: `{"file_path":"old.go"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "old"}}},
		{ID: "current", Role: message.User},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "missing", Name: "view", Input: `{"file_path":"missing.go"}`}, message.ToolCall{ID: "search", Name: "search_code_index", Input: `{"path":"index.go"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "missing", IsError: true}, message.ToolResult{ToolCallID: "search", Content: "source.go"}}},
	}
	require.Empty(t, missingSourceCitation(run, history, "The requested source could not be read."))
	history = append(history, message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "read", Name: "read_lines", Input: `{"file_path":"pkg/source.go"}`}}}, message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "read"}}})
	require.Contains(t, missingSourceCitation(run, history, "old.go:1, missing.go:2, index.go:1"), "pkg/source.go")
	require.Empty(t, missingSourceCitation(run, history, "pkg/source.go:2"))
	run.requiresCitation = false
	require.Empty(t, missingSourceCitation(run, history, "No code explanation was requested."))
}

func TestCitationHeuristicRespectsNegationAndExplicitReferenceOptOut(t *testing.T) {
	for _, request := range []string{"Explique este código", "Localize a causa do erro", "Explain this function", "Where is the bug?"} {
		require.True(t, requiresSourceCitation(request), request)
	}
	for _, request := range []string{"Não explique o código; responda OK", "Do not explain the file", "Don't explain this", "Explique em uma frase sem referências", "Explain the function but do not cite paths"} {
		require.False(t, requiresSourceCitation(request), request)
	}
	a := &agent{}
	run := &generationRun{mode: workflow.Ask}
	a.prefetchTools(run, "Explique a função")
	require.True(t, run.requiresCitation)
	a.prefetchTools(run, "Preserve o idioma português")
	require.True(t, run.requiresCitation)
	a.prefetchTools(run, "Não cite caminhos na resposta")
	require.False(t, run.requiresCitation, "a later user instruction overrides the default reference style")
}
