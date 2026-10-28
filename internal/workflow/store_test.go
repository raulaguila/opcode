package workflow_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestPlanSurvivesRestartAndMigrationPreservesHistory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	conn, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer conn.Close()
	goose.SetBaseFS(db.FS)
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.Up(conn, "migrations"))
	_, err = conn.Exec("INSERT INTO sessions(id,title,created_at,updated_at) VALUES('session','Retomar',0,0)")
	require.NoError(t, err)
	store := workflow.New(conn, root, workflow.Execute)
	_, err = store.SetMode(ctx, "session", workflow.Plan)
	require.NoError(t, err)
	steps := []workflow.Step{{Title: "Investigar", Status: "completed"}, {Title: "Implementar", Status: "pending"}}
	_, err = store.UpdatePlan(ctx, "session", "Objetivo", steps, []string{"Comportamento verificado"}, []string{"test"})
	require.NoError(t, err)
	_, err = store.Finish(ctx, "session", "planned", "Plano salvo", nil)
	require.NoError(t, err)
	reopened := workflow.New(conn, root, workflow.Execute)
	task, err := reopened.Get(ctx, "session")
	require.NoError(t, err)
	require.Equal(t, workflow.Plan, task.Mode)
	require.Equal(t, "planned", task.Status)
	require.Equal(t, steps, task.Steps)
	task.Steps[0].Title = "changed locally"
	task, err = reopened.Get(ctx, "session")
	require.NoError(t, err)
	require.Equal(t, "Investigar", task.Steps[0].Title)
	_, err = workflow.New(conn, t.TempDir(), workflow.Execute).Get(ctx, "session")
	require.ErrorContains(t, err, "another project")
	require.NoError(t, goose.Down(conn, "migrations"))
	var title string
	require.NoError(t, conn.QueryRow("SELECT title FROM sessions WHERE id='session'").Scan(&title))
	require.Equal(t, "Retomar", title)
	require.NoError(t, goose.Up(conn, "migrations"))
}

func TestCompletionRequiresCurrentRevisionAndExactCheckCommand(t *testing.T) {
	ctx := context.Background()
	store := workflow.New(nil, t.TempDir(), workflow.Execute)
	id := "task"
	steps := []workflow.Step{{Title: "Corrigir", Status: "completed"}, {Title: "Validar", Status: "pending"}}
	_, err := store.UpdatePlan(ctx, id, "Corrigir erro", steps, []string{"Teste passa"}, []string{"test"})
	require.NoError(t, err)
	_, err = store.Observe(ctx, id, "original")
	require.NoError(t, err)
	_, err = store.Observe(ctx, id, "changed")
	require.NoError(t, err)
	commands := map[string]string{"test": "go test ./..."}
	_, err = store.Finish(ctx, id, "completed", "pronto", commands)
	require.ErrorContains(t, err, "incomplete")
	steps[1].Status = "completed"
	_, err = store.UpdatePlan(ctx, id, "Corrigir erro", steps, []string{"Teste passa"}, []string{"test"})
	require.NoError(t, err)
	_, err = store.Finish(ctx, id, "completed", "pronto", commands)
	require.ErrorContains(t, err, "validation")
	require.NoError(t, store.RecordCheck(ctx, id, workflow.Receipt{Name: "test", Command: commands["test"], Digest: "original", Passed: true}))
	_, err = store.Finish(ctx, id, "completed", "pronto", commands)
	require.Error(t, err)
	require.NoError(t, store.RecordCheck(ctx, id, workflow.Receipt{Name: "test", Command: commands["test"], Digest: "changed", Passed: true}))
	_, err = store.Finish(ctx, id, "completed", "pronto", commands)
	require.NoError(t, err)
	_, err = store.Observe(ctx, id, "external edit")
	require.NoError(t, err)
	task, err := store.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "blocked", task.Status)
	require.NotEmpty(t, task.ValidationProblem(commands))
	require.NoError(t, store.RecordCheck(ctx, id, workflow.Receipt{Name: "test", Command: commands["test"], Digest: "external edit", Passed: true}))
	commands["test"] = "go test -race ./..."
	_, err = store.Finish(ctx, id, "completed", "pronto", commands)
	require.Error(t, err)
	_, err = store.SetMode(ctx, id, workflow.Plan)
	require.NoError(t, err)
	_, err = store.Finish(ctx, id, "completed", "pronto", commands)
	require.ErrorContains(t, err, "execute")
}

func TestPlanBoundsAndIndependentValidationReceipts(t *testing.T) {
	ctx := context.Background()
	s := workflow.New(nil, t.TempDir(), workflow.Execute)
	_, err := s.UpdatePlan(ctx, "s", "goal", []workflow.Step{{Title: "a", Status: "in_progress"}, {Title: "b", Status: "in_progress"}}, []string{"criterion"}, nil)
	require.Error(t, err)
	_, err = s.Observe(ctx, "s", "digest")
	require.NoError(t, err)
	for _, name := range []string{"a", "b"} {
		require.NoError(t, s.RecordCheck(ctx, "s", workflow.Receipt{Name: name, Command: name, Digest: "digest", Passed: true}))
	}
	require.NoError(t, s.InvalidateChecks(ctx, "s", []string{"b"}))
	task, err := s.Get(ctx, "s")
	require.NoError(t, err)
	require.Len(t, task.Validation, 1)
	require.Equal(t, "a", task.Validation[0].Name)
	_, err = s.SetMode(ctx, "s", workflow.Ask)
	require.NoError(t, err)
	_, err = s.UpdatePlan(ctx, "s", "goal", []workflow.Step{{Title: "a", Status: "pending"}}, []string{"criterion"}, nil)
	require.ErrorContains(t, err, "ask")
}
