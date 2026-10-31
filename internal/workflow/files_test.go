package workflow_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestFileReceiptsProveEffectsSeparatelyFromCheckReceipts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := workflow.New(nil, root, workflow.Execute)
	_, err := s.BeginTurn(ctx, "s", true)
	require.NoError(t, err)
	hash := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	file := workflow.FileReceipt{Path: filepath.Join(root, "new.txt"), After: hash("one")}
	require.NoError(t, s.RecordFileChanges(ctx, "s", []workflow.FileReceipt{file}))
	task, err := s.Get(ctx, "s")
	require.NoError(t, err)
	require.True(t, task.HasFileChanges())
	require.Empty(t, task.CompletionProblem())
	require.Empty(t, task.ValidationProblem(nil))
	require.False(t, task.NeedsWorkspaceVerification(nil))
	require.Empty(t, task.Validation)
	_, err = s.Finish(ctx, "s", "completed", "File saved and content verified; tests not configured", nil)
	require.NoError(t, err)
	require.NotEmpty(t, task.ValidationProblem(map[string]string{"test": "go test ./..."}))
	_, err = s.SetContract(ctx, "s", &workflow.Contract{Kind: "change", Checks: []string{"test"}})
	require.NoError(t, err)
	_, err = s.Finish(ctx, "s", "completed", "Cannot fake a test receipt", map[string]string{"test": "go test ./..."})
	require.ErrorContains(t, err, "validation")
	file.Before, file.After = file.After, hash("two")
	require.NoError(t, s.RecordFileChanges(ctx, "s", []workflow.FileReceipt{file}))
	task, err = s.Get(ctx, "s")
	require.NoError(t, err)
	require.Empty(t, task.FileChanges[0].Before)
	require.Equal(t, hash("two"), task.FileChanges[0].After)
	file.Before, file.After = file.After, ""
	require.NoError(t, s.RecordFileChanges(ctx, "s", []workflow.FileReceipt{file}))
	task, err = s.Get(ctx, "s")
	require.NoError(t, err)
	require.False(t, task.HasFileChanges(), "creating then removing a file is no net implementation")
}

func TestFileReceiptsInvalidateChecksAndResumeWithoutBecomingOldTurnProof(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := workflow.New(nil, root, workflow.Execute)
	_, err := s.Observe(ctx, "s", "workspace")
	require.NoError(t, err)
	_, err = s.BeginTurn(ctx, "s", true)
	require.NoError(t, err)
	require.NoError(t, s.RecordCheck(ctx, "s", workflow.Receipt{Name: "test", Command: "true", Digest: "workspace", Passed: true}))
	file := workflow.FileReceipt{Path: filepath.Join(root, "new.txt"), After: fmt.Sprintf("%x", sha256.Sum256(nil))}
	require.NoError(t, s.RecordFileChanges(ctx, "s", []workflow.FileReceipt{file}))
	task, err := s.Get(ctx, "s")
	require.NoError(t, err)
	require.Empty(t, task.Validation)
	require.Empty(t, task.Digest)
	_, err = s.BeginTurn(ctx, "s", false, true)
	require.NoError(t, err)
	task, err = s.Get(ctx, "s")
	require.NoError(t, err)
	require.Len(t, task.FileChanges, 1)
	require.NoError(t, s.RequireWorkspace(ctx, "s"))
	task, err = s.Get(ctx, "s")
	require.NoError(t, err)
	require.True(t, task.NeedsWorkspaceVerification(nil))
	_, err = s.BeginTurn(ctx, "s", true)
	require.NoError(t, err)
	task, err = s.Get(ctx, "s")
	require.NoError(t, err)
	require.Empty(t, task.FileChanges)
	require.False(t, task.WorkspaceRequired)
	require.NotEmpty(t, task.CompletionProblem())
}

func TestAffectedFileReceiptsSurviveStoreRestartWithoutPermissionsOrContents(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	conn, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	_, err = conn.Exec("CREATE TABLE agent_workflows (session_id TEXT PRIMARY KEY, root TEXT, state TEXT)")
	require.NoError(t, err)
	s := workflow.New(conn, root, workflow.Execute)
	_, err = s.BeginTurn(ctx, "s", true)
	require.NoError(t, err)
	file := workflow.FileReceipt{Path: filepath.Join(root, "new.txt"), After: fmt.Sprintf("%x", sha256.Sum256([]byte("private contents")))}
	require.NoError(t, s.RecordFileChanges(ctx, "s", []workflow.FileReceipt{file}))
	_, err = s.Finish(ctx, "s", "partial", "Continue later", nil)
	require.NoError(t, err)
	reopened := workflow.New(conn, root, workflow.Execute)
	task, err := reopened.BeginTurn(ctx, "s", true, true)
	require.NoError(t, err)
	require.Equal(t, []workflow.FileReceipt{file}, task.FileChanges)
	var raw string
	require.NoError(t, conn.QueryRow("SELECT state FROM agent_workflows WHERE session_id = 's'").Scan(&raw))
	require.NotContains(t, raw, "private contents")
	require.NotContains(t, raw, "permission")
	task.FileChanges[0].After = "modified by caller"
	task, err = reopened.Get(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, file.After, task.FileChanges[0].After)
}

func TestFileReceiptBoundsRejectInvalidOrOversizedStateAtomically(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := workflow.New(nil, root, workflow.Execute)
	for _, file := range []workflow.FileReceipt{{Path: "relative.txt", After: fmt.Sprintf("%x", sha256.Sum256(nil))}, {Path: filepath.Join(root, "a"), After: "not a hash"}, {Path: filepath.Join(root, "a")}} {
		require.Error(t, s.RecordFileChanges(ctx, "s", []workflow.FileReceipt{file}))
	}
	files := make([]workflow.FileReceipt, 128)
	for i := range files {
		files[i] = workflow.FileReceipt{Path: filepath.Join(root, fmt.Sprintf("%d.txt", i)), After: fmt.Sprintf("%x", sha256.Sum256(nil))}
	}
	require.NoError(t, s.RecordFileChanges(ctx, "s", files))
	overflow := workflow.FileReceipt{Path: filepath.Join(root, "overflow.txt"), After: files[0].After}
	require.ErrorContains(t, s.RecordFileChanges(ctx, "s", []workflow.FileReceipt{overflow}), "128")
	task, err := s.Get(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, files, task.FileChanges)
}
