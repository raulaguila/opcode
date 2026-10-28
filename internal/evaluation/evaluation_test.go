package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestBundledOfflineFixtureValidationNeverClaimsAgentQuality(t *testing.T) {
	suite, raw, err := Load("")
	require.NoError(t, err)
	require.Len(t, suite.Cases, 6)
	first, err := Run(context.Background(), suite, raw, Options{})
	require.NoError(t, err)
	second, err := Run(context.Background(), suite, raw, Options{})
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, "offline_fixture_validation", first.Mode)
	require.Contains(t, first.Notice, "not an agent quality score")
	for _, result := range first.Cases {
		require.Equal(t, "validated", result.Status)
		for _, criterion := range result.Criteria {
			require.Nil(t, criterion.Passed)
		}
	}
}
func TestFixturePreparationIsIsolatedAndRejectsTraversal(t *testing.T) {
	suite, _, err := Load("")
	require.NoError(t, err)
	base, root, err := Prepare(suite.Cases[0])
	require.NoError(t, err)
	defer os.RemoveAll(base)
	require.NotEqual(t, base, root)
	require.Equal(t, "workspace", filepath.Base(root))
	info, err := os.Stat(filepath.Join(root, "README.md"))
	require.NoError(t, err)
	require.EqualValues(t, 0600, info.Mode().Perm())
	for _, path := range []string{"../secret", "/absolute", ".opcode.json", "sub/.env", "a\\b", "."} {
		bad := suite.Cases[0]
		bad.Files = map[string]string{path: "bad"}
		require.Error(t, (Suite{Version: 1, Cases: []Case{bad}}).Validate())
		_, _, err := Prepare(bad)
		require.Error(t, err)
	}
}
func TestLiveRequiresExplicitProviderAndExecutionBeforeRequests(t *testing.T) {
	suite, raw, err := Load("")
	require.NoError(t, err)
	_, err = Run(context.Background(), suite, raw, Options{Live: true})
	require.ErrorContains(t, err, "--provider-config")
	_, err = Run(context.Background(), suite, raw, Options{Live: true, ProviderConfig: "not-read", Executable: "not-executed"})
	require.ErrorContains(t, err, "--allow-execution before any provider request")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Run(ctx, suite, raw, Options{})
	require.ErrorIs(t, err, context.Canceled)
}
func TestObservableCriteriaDoNotTreatHistoricalReceiptsAsCurrent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "answer.txt"), []byte("42"), 0600))
	c := Case{ID: "criteria", Criteria: []Criterion{{Kind: "response_contains", Value: "correct"}, {Kind: "file_contains", Path: "answer.txt", Value: "42"}, {Kind: "tool_called", Value: "run_checks"}, {Kind: "check_passed", Value: "answer"}}}
	observation := Observation{Response: "Correct answer", Tools: map[string]bool{"run_checks": true}, Task: workflow.Task{Revision: 3, Digest: "current", Validation: []workflow.Receipt{{Name: "answer", Passed: true, Revision: 2, Digest: "old"}}}}
	result := Evaluate(c, root, observation)
	require.Equal(t, "failed", result.Status)
	require.False(t, *result.Criteria[3].Passed)
	observation.Task.Validation[0].Revision = 3
	observation.Task.Validation[0].Digest = "current"
	result = Evaluate(c, root, observation)
	require.Equal(t, "passed", result.Status)
	outside := filepath.Join(t.TempDir(), "private")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))
	c.Criteria = []Criterion{{Kind: "file_contains", Path: "link", Value: "secret"}}
	require.Equal(t, "failed", Evaluate(c, root, observation).Status)
}
func TestEvaluationPermissionsAreScopedAndNeverAutomaticOutsideFixture(t *testing.T) {
	root := t.TempDir()
	request := permission.PermissionRequest{ToolName: "bash", Path: root, Action: "execute"}
	require.True(t, allowedPermission(root, true, "execute", request))
	require.False(t, allowedPermission(root, false, "execute", request))
	require.False(t, allowedPermission(root, true, "ask", request))
	require.False(t, allowedPermission(root, true, "plan", request))
	request.Path = filepath.Dir(root)
	require.False(t, allowedPermission(root, true, "execute", request))
	request.Path = "network:https://example.com"
	require.False(t, allowedPermission(root, true, "execute", request))
	request.Path = root
	request.ToolName = "fetch"
	require.False(t, allowedPermission(root, true, "execute", request))
}
func TestWorkerRejectsOriginalWorkspaceAndReportsContainNoPayload(t *testing.T) {
	root := t.TempDir()
	job := filepath.Join(root, "job.json")
	suite, _, err := Load("")
	require.NoError(t, err)
	raw, err := json.Marshal(Job{Workspace: root, Config: filepath.Join(root, "config.json"), Case: suite.Cases[0]})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(job, raw, 0600))
	var out strings.Builder
	require.ErrorContains(t, Worker(context.Background(), job, &out), "disposable")
	result := Evaluate(Case{ID: "private", Criteria: []Criterion{{Kind: "response_contains", Value: "answer"}}}, root, Observation{Response: "answer SECRET-SAMPLE-KEY"})
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SECRET-SAMPLE-KEY")
}
