package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type localPlanProvider struct{ provider.Provider }

func (localPlanProvider) Model() models.Model {
	return models.Model{ID: "local-plan", Provider: models.ProviderLocal, API: "ollama", ContextWindow: 16384, DefaultMaxTokens: 2048}
}
func TestNativeLocalPlanAdvertisesFlatSchemaAndSavesExactlyWhatModelSent(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Plan)
	a.provider = localPlanProvider{p}
	done, err := a.Run(context.Background(), "s", "Save a structured plan for the requested objective")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	found := false
	for _, tool := range first.exposed {
		info := tool.Info()
		if info.Name == "update_plan" {
			found = true
			require.Contains(t, info.Parameters, "steps_text")
			require.NotContains(t, info.Parameters, "steps")
			require.Contains(t, info.Description, "steps_text")
		}
	}
	require.True(t, found)
	replyTools(first, toolCall("update_plan", `{"objective":"Review exact objective","steps_text":"pending|Inspect source\npending|Review results","criteria_text":"Source inspected","checks_text":""}`), toolCall("finish_task", `{"status":"planned","summary":"Plan saved"}`))
	next := nextGeneration(t, p)
	next.reply <- provider.ProviderResponse{Content: "Plan saved", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	saved, _ := a.workflow.Get(context.Background(), "s")
	require.Equal(t, "Review exact objective", saved.Objective)
	require.Len(t, saved.Steps, 2)
	require.Equal(t, "planned", saved.Status)
}
func TestFlatPlanRejectsMissingFieldsAndNeverEvaluatesEncodedLiterals(t *testing.T) {
	run := &generationRun{provider: localPlanProvider{}}
	for _, raw := range []string{`{"objective":"X","steps":"[{}]"}`, `{"objective":"X","steps_text":"[{'title':'fake'}]","criteria_text":"x","checks_text":""}`, `{"objective":"X","steps_text":[],"criteria_text":"x","checks_text":""}`} {
		_, _, _, _, err := planArguments(run, raw)
		require.Error(t, err)
	}
}
func TestAppliedChangesWithoutConfiguredChecksArePartialNotFalseSuccess(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Execute)
	old := config.Get().Checks
	config.Get().Checks = nil
	t.Cleanup(func() { config.Get().Checks = old })
	done, err := a.Run(context.Background(), "s", "Corrija o arquivo")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.NoError(t, os.WriteFile(filepath.Join(root, "fixed.go"), []byte("package fixed\n"), 0600))
	first.reply <- provider.ProviderResponse{Content: "File changed", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, "partial", result.Outcome)
	saved, _ := a.workflow.Get(context.Background(), "s")
	require.Empty(t, saved.Validation)
	require.Equal(t, "partial", saved.Status)
	require.Contains(t, saved.Summary, "no configured checks")
}

func TestWrongCheckNameDoesNotCostThePlan(t *testing.T) {
	_, err := config.Load(t.TempDir(), false)
	require.NoError(t, err)
	store := workflow.New(nil, t.TempDir(), workflow.Execute)
	a := &agent{workflow: store}
	input := `{"objective":"Rodar testes","steps":[{"title":"Rodar","status":"in_progress"}],"criteria":["Rodam"],"checks":["test"]}`
	result, err := a.workflowUpdatePlan(&generationRun{mode: workflow.Execute}, "s", context.Background(), tools.ToolCall{Input: input})
	require.NoError(t, err)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(raw), "Plan saved without checks")

	task, err := store.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Len(t, task.Steps, 1)
	require.Empty(t, task.Checks, "a check nobody configured can never pass")
}
