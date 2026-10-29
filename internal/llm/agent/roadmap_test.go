package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestClosedBatchContinuityPreservesConstraintsAndNewestEvidence(t *testing.T) {
	instruction := message.Message{ID: "request", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Use Portuguese; never delete README; do not repeat completed commands"}}}
	history := []message.Message{instruction}
	for i := 0; i < 20; i++ {
		id := fmt.Sprint(i)
		history = append(history, message.Message{ID: "assistant-" + id, Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: id, Name: "view", Input: `{"file_path":"source.go"}`}}}, message.Message{ID: "result-" + id, Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: id, Content: strings.Repeat("source evidence\n", 1000)}}})
	}
	old, _ := json.Marshal(history)
	compacted, count := compactCompletedBatches(history)
	require.Equal(t, 38, count)
	require.Equal(t, instruction, compacted[0])
	require.Equal(t, history[len(history)-2:], compacted[len(compacted)-2:])
	newRaw, _ := json.Marshal(compacted)
	require.Less(t, len(newRaw), len(old)/4)
	for _, msg := range compacted[:len(compacted)-2] {
		require.Empty(t, msg.ToolCalls())
		require.Empty(t, msg.ToolResults())
	}
	require.Contains(t, string(newRaw), "read_conversation")
	require.Equal(t, 1000, strings.Count(history[2].ToolResults()[0].Content, "source evidence"))
}
func TestCatalogFilteringPaginationAndSchemaLease(t *testing.T) {
	a, _, _, _ := runtimeAgent(t, workflow.Ask)
	run := &generationRun{ctx: context.Background()}
	require.NoError(t, a.configureRun(run, "s"))
	result, err := toolCatalog(run, `{"read_only":true,"limit":2}`)
	require.NoError(t, err)
	raw, _ := json.Marshal(result)
	var page struct {
		Items []map[string]any
		Next  *int `json:"next_offset"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.Next)
	result, err = toolCatalog(run, `{"query":"permission","limit":16}`)
	require.NoError(t, err)
	require.NotNil(t, result)
	_, err = toolCatalog(run, `{"offset":-1}`)
	require.Error(t, err)
	run.selected["extra"] = true
	run.toolLeases["extra"] = 1
	run.steps = 3
	a.exposedTools(run)
	require.False(t, run.selected["extra"])
}
func TestRequirementCountsUseOnlyCurrentRunSuccessfulReceipts(t *testing.T) {
	run := &generationRun{available: []tools.BaseTool{&countedTool{name: "ls"}, &countedTool{name: "view"}}, startMessageID: "current", mode: workflow.Ask}
	requiredToolCalls("Use ls for the first page. Use ls for the next page. Use view to read the result.", run)
	require.Equal(t, 2, run.requiredCalls["ls"])
	history := []message.Message{{ID: "old", Role: message.User}, {Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "old-view", Name: "view"}}}, {Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "old-view"}}}, {ID: "current", Role: message.User}, {Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "ls", Name: "ls"}, message.ToolCall{ID: "view", Name: "view"}}}, {Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "ls"}, message.ToolResult{ToolCallID: "view", IsError: true}}}}
	missing := missingRequiredCalls(run, history)
	require.Len(t, missing, 2)
	require.Contains(t, strings.Join(missing, " "), "ls (1/2")
	require.Contains(t, strings.Join(missing, " "), "view (0/1")
}
func TestUsageCalibrationIncludesCachedInputAndKeepsUncertainty(t *testing.T) {
	a := &agent{contextStatuses: map[string]ContextStatus{}, tokenObservations: map[string]tokenObservation{}}
	model := models.Model{Provider: models.ProviderLocal, APIModel: "llama3.1:8b"}
	for i := 0; i < 3; i++ {
		a.contextStatuses["s"] = ContextStatus{EstimatedInputTokens: 1000, uncalibratedInput: 1000}
		a.observeInputUsage("s", model, provider.TokenUsage{InputTokens: 300, CacheReadTokens: 200, CacheCreationTokens: 100})
	}
	status := a.calibratedEstimate(model, ContextStatus{ContextWindow: 10000, EstimatedInputTokens: 1000})
	require.EqualValues(t, 720, status.EstimatedInputTokens)
	require.EqualValues(t, 600, status.ActualInputTokens)
	require.Contains(t, status.Method, "remains an estimate")
	a.observeUsage("s", provider.TokenUsage{InputTokens: 300, CacheReadTokens: 200, CacheCreationTokens: 100, OutputTokens: 50})
	usage, err := a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.EqualValues(t, 600, usage.InputTokens)
	require.EqualValues(t, 50, usage.OutputTokens)
}

type fixedBudgetProvider struct{ provider.Provider }

func (*fixedBudgetProvider) Model() models.Model {
	return models.Model{ID: "budget-fixture", ContextWindow: 16384, DefaultMaxTokens: 2048}
}
func TestActiveTurnBudgetCompactionCanResumeWithoutLosingUserConstraints(t *testing.T) {
	p := &fixedBudgetProvider{}
	a := &agent{provider: p, systemMessage: "Respect permissions and preserve README", maxOutputTokens: 2048}
	history := []message.Message{{ID: "objective", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Never delete README. Inspect the existing results before acting again."}}}}
	for i := 0; i < 8; i++ {
		id := fmt.Sprint(i)
		history = append(history, message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: id, Name: "view", Input: `{"file_path":"source.go"}`}}}, message.Message{ID: "receipt-" + id, Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: id, Content: strings.Repeat("completed source evidence\n", 350)}}})
	}
	require.False(t, estimateContext(p.Model(), a.systemMessage, history, nil, 2048).Fits)
	original, _ := json.Marshal(history)
	for i := 0; i < 2; i++ {
		run := &generationRun{provider: p, mode: workflow.Ask}
		prepared, err := a.budgetRequest("s", history, run)
		require.NoError(t, err)
		require.Equal(t, history[0], prepared[0])
		require.Equal(t, history[len(history)-2:], prepared[len(prepared)-2:])
		status, err := a.ContextStatus(context.Background(), "s")
		require.NoError(t, err)
		require.True(t, status.Fits)
		require.Greater(t, status.CompactedMessages, 0)
	}
	unchanged, _ := json.Marshal(history)
	require.Equal(t, original, unchanged)
}

func TestExplicitRequirementsNeverTurnNegationIntoAnAction(t *testing.T) {
	run := &generationRun{mode: workflow.Execute, available: []tools.BaseTool{&countedTool{name: "view"}, &countedTool{name: "bash"}, &countedTool{name: "edit"}, &countedTool{name: "patch"}, &countedTool{name: "write"}, &countedTool{name: "run_checks"}}}
	requiredToolCalls("Use view. Não use bash diretamente, use run_checks. Nunca chame edit. Do not call patch. Don't call write.", run)
	require.Equal(t, map[string]int{"view": 1, "run_checks": 1}, run.requiredCalls)
}
