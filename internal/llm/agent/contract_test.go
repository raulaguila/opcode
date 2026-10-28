package agent

import (
	"context"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExplicitExplainOverridesImplementationHeuristicAndRejectsForgedTools(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	write := &countedTool{name: "edit"}
	a.tools = []tools.BaseTool{write}
	_, err := a.workflow.SetContract(context.Background(), "s", &workflow.Contract{Kind: "explain"})
	require.NoError(t, err)
	done, err := a.Run(context.Background(), "s", "Fix the file: explain the fix first")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.NotContains(t, toolNames(first), "edit")
	replyTools(first, toolCall("edit", `{}`))
	next := nextGeneration(t, p)
	require.True(t, next.history[len(next.history)-1].ToolResults()[0].IsError)
	next.reply <- provider.ProviderResponse{Content: "Explanation", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	require.Zero(t, write.count.Load())
}
func TestVerifyContractCannotLoadOrCallWriteTools(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	write := &countedTool{name: "edit"}
	checks := &countedTool{name: "run_checks"}
	a.tools = []tools.BaseTool{write, checks}
	_, err := a.workflow.SetContract(context.Background(), "s", &workflow.Contract{Kind: "verify", Checks: []string{"test"}})
	require.NoError(t, err)
	done, err := a.Run(context.Background(), "s", "Corrija o arquivo e teste")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.NotContains(t, toolNames(first), "edit")
	require.Contains(t, toolNames(first), "run_checks")
	replyTools(first, toolCall("request_tools", `{"names":["edit"]}`), toolCall("edit", `{}`), toolCall("finish_task", `{"status":"blocked","summary":"Check unavailable"}`))
	next := nextGeneration(t, p)
	results := next.history[len(next.history)-1].ToolResults()
	require.True(t, results[0].IsError)
	require.True(t, results[1].IsError)
	next.reply <- provider.ProviderResponse{Content: "Blocked", FinishReason: message.FinishReasonEndTurn}
	require.Equal(t, "blocked", finishGeneration(t, done).Outcome)
	require.Zero(t, write.count.Load())
}
func TestExplicitChangeOverridesAmbiguousTextAndKeepsCompletionGuard(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	a.tools = []tools.BaseTool{&countedTool{name: "edit"}}
	_, err := a.workflow.SetContract(context.Background(), "s", &workflow.Contract{Kind: "change"})
	require.NoError(t, err)
	done, err := a.Run(context.Background(), "s", "The requested adjustment")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.Contains(t, toolNames(first), "edit")
	replyTools(first, toolCall("finish_task", `{"status":"partial","summary":"No changes yet"}`))
	next := nextGeneration(t, p)
	next.reply <- provider.ProviderResponse{Content: "Partial", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.Equal(t, "partial", result.Outcome)
	task, _ := a.workflow.Get(context.Background(), "s")
	require.True(t, task.RequiresChanges)
}

func TestExplicitContractNeverClaimsChangeOrVerificationInReadOnlyMode(t *testing.T) {
	for _, kind := range []string{"change", "verify", "plan"} {
		a, p, _, _ := runtimeAgent(t, workflow.Ask)
		contract := &workflow.Contract{Kind: kind}
		if kind == "verify" {
			contract.Checks = []string{"test"}
		}
		_, err := a.workflow.SetContract(context.Background(), "s", contract)
		require.NoError(t, err)
		_, err = a.Run(context.Background(), "s", "Do the task")
		require.ErrorContains(t, err, "mode")
		select {
		case <-p.calls:
			t.Fatal("provider called for incompatible mode")
		default:
		}
	}
}
