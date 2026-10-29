package evaluation

import (
	"encoding/json"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestComparisonAggregatesOnlyCompletePhaseObservations(t *testing.T) {
	yes := true
	phase := &agent.PhaseMetrics{ProviderMS: 20, FirstTokenMS: 5, FirstTokenSamples: 1, ProviderCalls: 1, ToolsMS: 3, ToolCalls: 1}
	c := CaseResult{ID: "one", Application: "opcode", APIModel: "m", Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048, Status: "passed", Criteria: []CriterionResult{{Kind: "response_contains", Passed: &yes}}, PhaseMetrics: phase}
	a := Report{Version: 1, Mode: "live_provider", SuiteSHA256: "08b9aff679c7f9743feb5019658ffde63e9a9f679b566037edc016054860db24", Cases: []CaseResult{c, c}}
	a.Cases[0].Trial, a.Cases[1].Trial = 1, 2
	b := a
	b.Cases = append([]CaseResult(nil), a.Cases...)
	for i := range b.Cases {
		b.Cases[i].Application = "codaro"
	}
	b.Cases[1].PhaseMetrics = nil
	comparison, err := Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.NoError(t, err)
	require.True(t, comparison.Comparable)
	require.NotNil(t, comparison.Runs[0].PhaseMetrics)
	require.EqualValues(t, 40, comparison.Runs[0].PhaseMetrics.ProviderMS)
	require.EqualValues(t, 2, comparison.Runs[0].PhaseMetrics.FirstTokenSamples)
	require.Nil(t, comparison.Runs[1].PhaseMetrics)
	raw, err := json.Marshal(comparison.Runs[1])
	require.NoError(t, err)
	require.Contains(t, string(raw), `"phase_metrics":null`)
	a.Cases[0].PhaseMetrics = &agent.PhaseMetrics{ProviderMS: -1}
	_, err = Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.ErrorContains(t, err, "invalid phase metrics")
}
func TestPhaseAggregationRejectsOverflowWithoutMutation(t *testing.T) {
	total := agent.PhaseMetrics{ProviderMS: math.MaxInt64}
	require.False(t, addPhaseMetrics(&total, agent.PhaseMetrics{ProviderMS: 1, ContextMS: 1}))
	require.Zero(t, total.ContextMS)
	require.EqualValues(t, math.MaxInt64, total.ProviderMS)
}
