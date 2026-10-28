package workflow

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExplicitContractPersistsSeparatelyFromPlanAndRequiresCurrentChecks(t *testing.T) {
	s := New(nil, t.TempDir(), Execute)
	ctx := context.Background()
	c := &Contract{Kind: "verify", Targets: []string{"main.go"}, Acceptance: []string{"Expected check passes"}, Checks: []string{"test"}}
	task, err := s.SetContract(ctx, "s", c)
	require.NoError(t, err)
	c.Targets[0] = "changed"
	require.Equal(t, "main.go", task.Contract.Targets[0])
	task.Contract.Targets[0] = "also changed"
	task, err = s.Get(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, "main.go", task.Contract.Targets[0])
	require.Contains(t, task.ValidationProblem(map[string]string{"test": "go test ./..."}), "test")
	_, err = s.Observe(ctx, "s", "digest")
	require.NoError(t, err)
	require.NoError(t, s.RecordCheck(ctx, "s", Receipt{Name: "test", Command: "go test ./...", Digest: "digest", Passed: true}))
	task, _ = s.Get(ctx, "s")
	require.Empty(t, task.ValidationProblem(map[string]string{"test": "go test ./..."}))
	require.NotEmpty(t, task.ValidationProblem(map[string]string{"test": "different command"}))
	_, err = s.Reset(ctx, "s")
	require.NoError(t, err)
	task, _ = s.Get(ctx, "s")
	require.Nil(t, task.Contract)
}
func TestContractCannotRaiseModeOrAcceptEscapingPaths(t *testing.T) {
	for _, c := range []Contract{{Kind: "change", Targets: []string{"../outside"}}, {Kind: "verify"}, {Kind: "unknown"}, {Kind: "explain", Targets: []string{"/absolute"}}} {
		require.Error(t, c.Validate())
	}
	require.Equal(t, Ask, (Task{Mode: Execute, Contract: &Contract{Kind: "explain"}}).EffectiveMode())
	require.Equal(t, Ask, (Task{Mode: Ask, Contract: &Contract{Kind: "change"}}).EffectiveMode())
	require.Equal(t, Ask, (Task{Mode: Ask, Contract: &Contract{Kind: "plan"}}).EffectiveMode())
}
