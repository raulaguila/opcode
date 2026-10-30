package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The child is a controlled contract adapter, not a real application/model
// evaluation. It verifies the executable stdin/stdout path in fresh workspaces.
func TestExperimentAdapterHelper(t *testing.T) {
	mode := ""
	for i, arg := range os.Args {
		if arg == "--experiment-helper" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}
	if mode == "" {
		return
	}
	if mode == "survivor" {
		marker := os.Args[len(os.Args)-1]
		_ = os.WriteFile(marker+".ready", []byte("ready"), 0600)
		time.Sleep(700 * time.Millisecond)
		_ = os.WriteFile(marker, []byte("escaped timeout"), 0600)
		os.Exit(0)
	}
	var job Job
	if json.NewDecoder(os.Stdin).Decode(&job) != nil {
		os.Exit(3)
	}
	zero := 0.0
	observed := AdapterObservation{Conditions: job.Conditions, Response: "OK", Temperature: &zero, EffectiveContextWindow: &job.Conditions.ContextWindow, EffectiveOutputReserve: &job.Conditions.OutputReserve}
	if mode == "timeout" {
		child := exec.Command(os.Args[0], "-test.run=^TestExperimentAdapterHelper$", "--", "--experiment-helper", "survivor", filepath.Join(job.Workspace, "survived"))
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(6)
		}
		time.Sleep(10 * time.Second)
	}
	if mode == "unobserved" {
		observed.EffectiveContextWindow, observed.EffectiveOutputReserve = nil, nil
	}
	if mode == "drift" {
		window := int64(8192)
		observed.EffectiveContextWindow = &window
	}
	if mode == "unavailable" {
		observed.Unavailable = "fixture capability unsupported"
		observed.Temperature = nil
	}
	if mode == "fail" {
		os.Exit(4)
	}
	if json.NewEncoder(os.Stdout).Encode(observed) != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

func experimentFixture(t *testing.T) (string, ExperimentManifest) {
	t.Helper()
	dir := t.TempDir()
	suite := Suite{Version: 1, Cases: []Case{{ID: "one", Mode: "ask", Files: map[string]string{"README.md": "fixture"}, Prompts: []string{"Respond OK"}, Criteria: []Criterion{{Kind: "response_contains", Value: "OK"}}}}}
	raw, err := json.Marshal(suite)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), raw, 0600))
	sum := sha256.Sum256(raw)
	provider := `{"providers":{"local":{"baseURL":"http://127.0.0.1:11434/v1"}},"models":{"local.tiny":{"provider":"local","api_model":"tiny","api":"ollama","default_max_tokens":1024}},"agents":{"coder":{"model":"local.tiny"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "provider.json"), []byte(provider), 0600))
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, name := range []string{"codaro", "crush"} {
		a := Adapter{Name: name, Command: binary, Args: []string{"-test.run=^TestExperimentAdapterHelper$", "--", "--experiment-helper", "ok"}}
		encoded, _ := json.Marshal(a)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+"-adapter.json"), encoded, 0600))
	}
	zero := 0.0
	m := ExperimentManifest{Version: 1, Name: "shared-test", Suite: "suite.json", SuiteSHA256: hex.EncodeToString(sum[:]), ProviderConfig: "provider.json", Conditions: SharedConditions{APIModel: "tiny", Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048, Temperature: &zero}, Repeats: 2, Cases: []string{"one"}, Applications: []ExperimentApplication{{Name: "codaro", Adapter: "codaro-adapter.json"}, {Name: "crush", Adapter: "crush-adapter.json"}}}
	return dir, m
}

func saveManifest(t *testing.T, dir string, m ExperimentManifest) string {
	t.Helper()
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	path := filepath.Join(dir, "experiment.json")
	require.NoError(t, os.WriteFile(path, raw, 0600))
	return path
}

func TestSharedExperimentRunsIdenticalCasesAndPreservesObservedDrift(t *testing.T) {
	for _, mode := range []string{"ok", "drift", "unavailable", "fail", "missing", "unobserved"} {
		t.Run(mode, func(t *testing.T) {
			testSharedExperimentMode(t, mode)
		})
	}
}

func TestExperimentSnapshotsInputsAndPreservesCancelledSummary(t *testing.T) {
	dir, m := experimentFixture(t)
	m.Repeats = 1
	x, err := LoadExperiment(saveManifest(t, dir, m))
	require.NoError(t, err)
	// Original files may disappear/change after loading; every application must
	// still use the already validated provider and fixture bytes.
	require.NoError(t, os.Remove(filepath.Join(dir, "provider.json")))
	require.NoError(t, os.Remove(filepath.Join(dir, "suite.json")))
	r, err := x.Run(context.Background(), os.Args[0], filepath.Join(dir, "snapshot"), true, false)
	require.NoError(t, err)
	require.True(t, r.Comparison.Rankable)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output := filepath.Join(dir, "cancelled")
	_, err = x.Run(ctx, os.Args[0], output, true, false)
	require.ErrorIs(t, err, context.Canceled)
	var partial ExperimentReport
	raw, err := os.ReadFile(filepath.Join(output, "experiment.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &partial))
	require.Equal(t, "live_experiment", partial.Mode)
	require.Equal(t, "planned", partial.Applications[0].Status)
	require.Nil(t, partial.Comparison)
}

func TestExperimentRejectsSymlinkOutputParentBeforeExecuting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require privileges")
	}
	dir, m := experimentFixture(t)
	x, err := LoadExperiment(saveManifest(t, dir, m))
	require.NoError(t, err)
	real := filepath.Join(dir, "real")
	require.NoError(t, os.Mkdir(real, 0700))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(real, link))
	_, err = x.Run(context.Background(), os.Args[0], filepath.Join(link, "reports"), true, false)
	require.ErrorContains(t, err, "without symbolic links")
	_, err = os.Stat(filepath.Join(real, "reports"))
	require.True(t, os.IsNotExist(err))
}

func TestAdapterCancellationStopsOrdinarySubprocesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process group cancellation")
	}
	dir := t.TempDir()
	adapter := &Adapter{Name: "codaro", Command: os.Args[0], Args: []string{"-test.run=^TestExperimentAdapterHelper$", "--", "--experiment-helper", "timeout"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := adapterCommand(ctx, adapter, dir, Job{Workspace: dir}, os.Environ())
		done <- err
	}()
	marker := filepath.Join(dir, "survived")
	require.Eventually(t, func() bool { _, err := os.Stat(marker + ".ready"); return err == nil }, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err) // Adapter failures become explicit failed results.
	case <-time.After(3 * time.Second):
		t.Fatal("adapter cancellation did not bound process/pipe lifetime")
	}
	time.Sleep(800 * time.Millisecond)
	_, err := os.Stat(marker)
	require.True(t, os.IsNotExist(err), "ordinary subprocess continued after adapter cancellation")
}

func TestExperimentPlanAndPreflightHaveNoExecutionOrOutputEffects(t *testing.T) {
	dir, m := experimentFixture(t)
	x, err := LoadExperiment(saveManifest(t, dir, m))
	require.NoError(t, err)
	output := filepath.Join(dir, "must-not-exist")
	r, err := x.Run(context.Background(), "not-executed", output, false, false)
	require.NoError(t, err)
	require.Equal(t, "experiment_plan", r.Mode)
	require.Nil(t, r.Comparison)
	_, err = os.Stat(output)
	require.True(t, os.IsNotExist(err))
	m.Conditions.APIModel = "different"
	_, err = LoadExperiment(saveManifest(t, dir, m))
	require.ErrorContains(t, err, "model differs")
	m.Conditions.APIModel = "tiny"
	m.Cases = []string{"one", "one"}
	_, err = LoadExperiment(saveManifest(t, dir, m))
	require.ErrorContains(t, err, "unique")
	m.Cases = []string{"one"}
	m.SuiteSHA256 = strings.Repeat("0", 64)
	_, err = LoadExperiment(saveManifest(t, dir, m))
	require.ErrorContains(t, err, "suite_sha256")
}

func TestComparisonRejectsDuplicateTrialsAndExplainsUnknownSampling(t *testing.T) {
	zero, yes := 0.0, true
	c := CaseResult{ID: "validate", Trial: 1, Application: "opcode", APIModel: "m", Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048, Temperature: &zero, CriteriaSHA256: strings.Repeat("a", 64), Status: "passed", Criteria: []CriterionResult{{Kind: "response_contains", Passed: &yes}}}
	a := Report{Version: 1, Mode: "live_provider", SuiteSHA256: strings.Repeat("a", 64), Cases: []CaseResult{c}}
	b := a
	b.Cases = append([]CaseResult(nil), a.Cases...)
	b.Cases[0].Application = "codaro"
	b.Cases[0].Temperature = nil
	comp, err := Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.NoError(t, err)
	require.Contains(t, strings.Join(comp.Differences, "\n"), "codaro case validate trial 1: temperature differs")
	require.False(t, comp.Rankable)
	b.Cases = append(b.Cases, b.Cases[0])
	_, err = Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.ErrorContains(t, err, "duplicate case/trial")
	b.Cases = b.Cases[:1]
	b.Repeats = 2
	b.SelectedCases = []string{"validate"}
	_, err = Compare([]string{reportFile(t, a), reportFile(t, b)})
	require.ErrorContains(t, err, "missing case/trial validate/2")
}

func TestComparisonRejectsNegativeAndOverflowedMeasurements(t *testing.T) {
	yes := true
	zero := 0.0
	negative, maximum := int64(-1), int64(math.MaxInt64)
	for _, metric := range []string{"negative", "input", "output", "duration", "tools"} {
		t.Run(metric, func(t *testing.T) {
			c := CaseResult{ID: "first", Application: "opcode", APIModel: "m", Protocol: "ollama", ContextWindow: 16384, OutputReserve: 2048, Status: "passed", Temperature: &zero, CriteriaSHA256: strings.Repeat("a", 64), Criteria: []CriterionResult{{Kind: "response_contains", Passed: &yes}}}
			switch metric {
			case "negative":
				c.InputTokens = &negative
			case "input":
				c.InputTokens = &maximum
			case "output":
				c.OutputTokens = &maximum
			case "duration":
				c.DurationMS = math.MaxInt64
			case "tools":
				c.ToolCalls = math.MaxInt
			}
			second := c
			second.ID = "second"
			r := Report{Version: 1, Mode: "live_provider", SuiteSHA256: strings.Repeat("a", 64), Cases: []CaseResult{c, second}}
			_, err := Compare([]string{reportFile(t, r), reportFile(t, r)})
			require.Error(t, err)
			if metric == "negative" {
				require.ErrorContains(t, err, "negative")
			} else {
				require.ErrorContains(t, err, "overflow")
			}
		})
	}
}

func testSharedExperimentMode(t *testing.T, mode string) {
	t.Helper()
	dir, m := experimentFixture(t)
	var a Adapter
	raw, err := os.ReadFile(filepath.Join(dir, "crush-adapter.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &a))
	a.Args[len(a.Args)-1] = mode
	if mode == "missing" {
		a.Command = filepath.Join(dir, "not-installed")
	}
	raw, _ = json.Marshal(a)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crush-adapter.json"), raw, 0600))
	x, err := LoadExperiment(saveManifest(t, dir, m))
	require.NoError(t, err)
	output := filepath.Join(dir, "reports")
	report, err := x.Run(context.Background(), os.Args[0], output, true, false)
	require.NoError(t, err)
	require.NotNil(t, report.Comparison)
	if mode == "ok" {
		require.True(t, report.Comparison.Comparable)
		require.True(t, report.Comparison.Rankable)
	} else {
		require.False(t, report.Comparison.Rankable)
	}
	if mode == "drift" {
		require.Contains(t, strings.Join(report.Comparison.Differences, "\n"), "context_window differs from manifest")
	}
	if mode == "unobserved" {
		require.Contains(t, strings.Join(report.Comparison.RankingUnavailable, "\n"), "effective context_window unavailable")
	}
	if mode == "unavailable" || mode == "missing" {
		require.Positive(t, report.Comparison.Runs[1].Unavailable)
	}
	for _, name := range []string{"codaro.json", "crush.json", "experiment.json"} {
		info, e := os.Stat(filepath.Join(output, name))
		require.NoError(t, e)
		if os.PathSeparator == '/' {
			require.EqualValues(t, 0600, info.Mode().Perm())
		}
	}
	var observed Report
	raw, err = os.ReadFile(filepath.Join(output, "codaro.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &observed))
	require.Len(t, observed.Cases, 2)
	require.EqualValues(t, 2048, observed.Cases[0].OutputReserve)
	require.Len(t, observed.Cases[0].CriteriaSHA256, 64)
	require.Equal(t, 1, observed.Cases[0].Trial)
	require.Equal(t, 2, observed.Cases[1].Trial)
	_, err = x.Run(context.Background(), os.Args[0], output, true, false)
	require.ErrorContains(t, err, "must not already exist")
}
