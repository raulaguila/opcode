package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func reportFile(t *testing.T, report Report) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.json")
	raw, err := json.Marshal(report)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))
	return path
}

func TestComparisonDiagnosticsRedactRegisteredCredentialsWithoutHidingDifferences(t *testing.T) {
	const first = "opaque-fixture-comparison-private-first"
	const second = "opaque-fixture-comparison-private-second\\with\"quote"
	config.RegisterRuntimeCredentials(t.Name(), first, second)
	t.Cleanup(func() { config.RegisterRuntimeCredentials(t.Name()) })
	yes, zero := true, 0.0
	c := CaseResult{ID: "one", Application: "opcode", APIModel: first, Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048, Temperature: &zero, CriteriaSHA256: strings.Repeat("a", 64), Status: "passed", Criteria: []CriterionResult{{Kind: "file_contains", Path: first, Passed: &yes}}}
	a := Report{Version: 1, Mode: "live_provider", SuiteSHA256: strings.Repeat("a", 64), Cases: []CaseResult{c}}
	b := a
	b.Cases = append([]CaseResult(nil), a.Cases...)
	b.Cases[0].Application, b.Cases[0].APIModel = "codaro", second
	b.Cases[0].Criteria = []CriterionResult{{Kind: "file_contains", Path: second, Passed: &yes}}
	b.RequestedConditions = &SharedConditions{APIModel: first, Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048, Temperature: &zero}
	r, err := Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.NoError(t, err)
	require.False(t, r.Comparable, "redacted display values must never participate in equality")
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	require.NotContains(t, string(raw), first)
	require.NotContains(t, string(raw), second)
	for _, secret := range []string{first, second} {
		encoded, err := json.Marshal(secret)
		require.NoError(t, err)
		require.NotContains(t, string(raw), string(encoded[1:len(encoded)-1]))
	}
	require.Contains(t, strings.Join(r.Differences, "\n"), "api_model differs")
	require.Contains(t, strings.Join(r.Differences, "\n"), "criteria differs")
	require.Contains(t, strings.Join(r.Differences, "\n"), "differs from manifest")
	// Imported malformed cases must not leak their identifiers through errors.
	b.Cases[0].ID = second
	b.Cases = append(b.Cases, b.Cases[0])
	_, err = Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.Error(t, err)
	require.NotContains(t, err.Error(), second)
}

func TestComparisonPreservesUnavailableCriteriaAndRefusesDifferentConditions(t *testing.T) {
	yes, no := true, false
	a := Report{Version: 1, Mode: "live_provider", SuiteSHA256: "08b9aff679c7f9743feb5019658ffde63e9a9f679b566037edc016054860db24", Cases: []CaseResult{{ID: "overview", Application: "opcode", ApplicationVersion: "0.2.0-test", APIModel: "llama3.1:8b", Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048, Status: "passed", Criteria: []CriterionResult{{Kind: "response_contains", Passed: &yes}, {Kind: "tool_called", Passed: &yes}}, PromptVersion: "opcode-2026-10-v3", PromptProfile: "reduced"}}}
	b := a
	b.Cases = append([]CaseResult(nil), a.Cases...)
	b.Cases[0].Application = "codaro"
	b.Cases[0].Status = "unavailable"
	b.Cases[0].Criteria = []CriterionResult{{Kind: "response_contains", Passed: &yes}, {Kind: "tool_called"}}
	comp, err := Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.NoError(t, err)
	require.True(t, comp.Comparable)
	require.Equal(t, 1, comp.Runs[1].Unavailable)
	require.Equal(t, 1, comp.Runs[1].PassedCriteria)
	require.Equal(t, 1, comp.Runs[1].UnavailableCriteria)
	require.Nil(t, comp.Runs[1].InputTokens)
	require.Nil(t, comp.Runs[1].RetrievalRecall)
	b.Cases[0].ContextWindow = 131072
	b.Cases[0].Criteria[0].Passed = &no
	comp, err = Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.NoError(t, err)
	require.False(t, comp.Comparable)
	require.NotEmpty(t, comp.Differences)
	a.Mode = "offline_fixture_validation"
	_, err = Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.ErrorContains(t, err, "live report")
}

func TestAdapterUnavailableIsNeverPassAndHasNoInventedMetrics(t *testing.T) {
	workspace := t.TempDir()
	job := Job{Case: Case{ID: "unsupported", Criteria: []Criterion{{Kind: "tool_called", Value: "view"}}}, Conditions: Conditions{Model: "local.llama3.1:8b", APIModel: "llama3.1:8b", Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048}}
	observation := AdapterObservation{Conditions: job.Conditions, Unavailable: "not available"}
	raw, err := json.Marshal(observation)
	require.NoError(t, err)
	script := filepath.Join(t.TempDir(), "adapter")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' '"+string(raw)+"'\n"), 0700))
	r, err := adapterCommand(context.Background(), &Adapter{Name: "crush", Command: script}, workspace, job, os.Environ())
	require.NoError(t, err)
	require.Equal(t, "unavailable", r.Status)
	require.Nil(t, r.Criteria[0].Passed)
	require.Nil(t, r.InputTokens)
	require.Equal(t, "llama3.1:8b", r.APIModel)
}

func TestAdapterRetainsObservedFailureAlongsideUnavailableCriterion(t *testing.T) {
	workspace := t.TempDir()
	job := Job{Case: Case{ID: "partial", Criteria: []Criterion{{Kind: "response_contains", Value: "correct"}, {Kind: "tool_called", Value: "view"}}}, Conditions: Conditions{Model: "m", ContextWindow: 16384}}
	observation := AdapterObservation{Conditions: job.Conditions, Response: "incorrect answer", UnsupportedCriteria: []string{"tool_called"}}
	// Use an unmatched phrase: unavailable tool evidence must not hide an observed failure.
	job.Case.Criteria[0].Value = "resolved-source"
	raw, err := json.Marshal(observation)
	require.NoError(t, err)
	script := filepath.Join(t.TempDir(), "adapter")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' '"+string(raw)+"'\n"), 0700))
	r, err := adapterCommand(context.Background(), &Adapter{Name: "crush", Command: script}, workspace, job, os.Environ())
	require.NoError(t, err)
	require.Equal(t, "failed", r.Status)
	require.False(t, *r.Criteria[0].Passed)
	require.Nil(t, r.Criteria[1].Passed)
}

func TestExperimentLabelCannotCarryFreeTextOrSecrets(t *testing.T) {
	suite, raw, err := Load("")
	require.NoError(t, err)
	_, err = Run(context.Background(), suite, raw, Options{Experiment: "reduced prompt + key=secret"})
	require.ErrorContains(t, err, "ASCII label")
}

func TestExternalAdapterEnvironmentExcludesUnrelatedCredentialsAndStartupInjection(t *testing.T) {
	env := adapterEnvironment([]string{"PATH=/usr/bin", "HOME=/tmp/fixture", "OPENAI_API_KEY=explicit", "AWS_SECRET_ACCESS_KEY=unrelated", "PYTHONPATH=/untrusted", "CRUSH_HOST=foreign", "LOCAL_ENDPOINT=foreign"}, map[string]bool{"OPENAI_API_KEY": true})
	require.ElementsMatch(t, []string{"PATH=/usr/bin", "HOME=/tmp/fixture", "OPENAI_API_KEY=explicit"}, env)
	require.True(t, Failed(Report{Cases: []CaseResult{{Status: "unavailable"}}}))
}
