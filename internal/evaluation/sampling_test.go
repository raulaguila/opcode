package evaluation

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestComparisonSeparatesDifferentOrUnknownSampling(t *testing.T) {
	zero, tenth, yes := 0.0, 0.1, true
	report := Report{Version: 1, Mode: "live_provider", SuiteSHA256: "08b9aff679c7f9743feb5019658ffde63e9a9f679b566037edc016054860db24", Cases: []CaseResult{{ID: "overview", Application: "opcode", APIModel: "llama3.1:8b", Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048, Temperature: &zero, Status: "passed", Criteria: []CriterionResult{{Kind: "response_contains", Passed: &yes}}}}}
	first := reportFile(t, report)
	report.Cases[0].Temperature = &tenth
	different, err := Compare([]string{first, reportFile(t, report)})
	require.NoError(t, err)
	require.False(t, different.Comparable)
	report.Cases[0].Temperature = nil
	unknown, err := Compare([]string{first, reportFile(t, report)})
	require.NoError(t, err)
	require.False(t, unknown.Comparable, "unknown sampling is not silently zero")
	report.Cases[0].Temperature = &zero
	matched, err := Compare([]string{first, reportFile(t, report)})
	require.NoError(t, err)
	require.True(t, matched.Comparable)
	require.Equal(t, []float64{0}, matched.Runs[0].Temperatures)
}
