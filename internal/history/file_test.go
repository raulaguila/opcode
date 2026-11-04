package history

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/stretchr/testify/require"
)

func historyFixture(t *testing.T) Service {
	t.Helper()
	connection, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "history.sqlite"))
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { connection.Close() })
	goose.SetBaseFS(db.FS)
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.Up(connection, "migrations"))
	_, err = connection.Exec(`INSERT INTO sessions(id,title,created_at,updated_at) VALUES('A','One',1,1),('B','Two',1,1)`)
	require.NoError(t, err)
	return NewService(db.New(connection), connection)
}

func TestVersionsCountUpAndTheLatestWins(t *testing.T) {
	service, ctx := historyFixture(t), context.Background()
	first, err := service.CreateVersion(ctx, "A", "/p/a.go", "one")
	require.NoError(t, err)
	require.Equal(t, InitialVersion, first.Version)
	for i, want := range []string{"v1", "v2", "v3"} {
		file, err := service.CreateVersion(ctx, "A", "/p/a.go", string(rune('a'+i)))
		require.NoError(t, err)
		require.Equal(t, want, file.Version)
	}
	latest, err := service.GetByPathAndSession(ctx, "/p/a.go", "A")
	require.NoError(t, err)
	require.Equal(t, "c", latest.Content)
	require.Equal(t, "v3", latest.Version)
	files, err := service.ListLatestSessionFiles(ctx, "A")
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "c", files[0].Content)
	all, err := service.ListBySession(ctx, "A")
	require.NoError(t, err)
	require.Len(t, all, 4)
}

func TestDeleteSessionFilesLeavesOtherSessions(t *testing.T) {
	service, ctx := historyFixture(t), context.Background()
	_, err := service.Create(ctx, "A", "/p/a.go", "a")
	require.NoError(t, err)
	kept, err := service.Create(ctx, "B", "/p/b.go", "b")
	require.NoError(t, err)
	require.NoError(t, service.DeleteSessionFiles(ctx, "A"))
	files, err := service.ListBySession(ctx, "A")
	require.NoError(t, err)
	require.Empty(t, files)
	found, err := service.Get(ctx, kept.ID)
	require.NoError(t, err)
	require.Equal(t, "b", found.Content)
}
