package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// openStore connects the way the app does: the real DSN pragmas, migrations
// and connection pool, on a folder the test owns.
func openStore(t *testing.T) func() *sql.DB {
	t.Helper()
	root := t.TempDir()
	file := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(file, []byte(`{}`), 0600))
	old := viper.ConfigFileUsed()
	viper.SetConfigFile(file)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := config.Load(root, false)
	require.NoError(t, err)
	data := cfg.Data.Directory
	cfg.Data.Directory = filepath.Join(root, "data")
	t.Cleanup(func() { cfg.Data.Directory = data; viper.SetConfigFile(old) })
	return func() *sql.DB {
		connection, err := db.Connect()
		require.NoError(t, err)
		return connection
	}
}

func TestConversationSurvivesRestartAndDeletesWhole(t *testing.T) {
	connect := openStore(t)
	ctx := context.Background()
	connection := connect()
	sessions, messages := NewService(db.New(connection)), message.NewService(db.New(connection))
	created, err := sessions.Create(ctx, "Persist")
	require.NoError(t, err)
	for _, text := range []string{"one", "two", "three"} {
		_, err := messages.Create(ctx, created.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}})
		require.NoError(t, err)
	}
	created.Title, created.PromptTokens = "Renamed", 42
	_, err = sessions.Save(ctx, created)
	require.NoError(t, err)
	require.NoError(t, connection.Close())

	connection = connect() // a restart: nothing may live only in memory
	defer connection.Close()
	sessions, messages = NewService(db.New(connection)), message.NewService(db.New(connection))
	reopened, err := sessions.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed", reopened.Title)
	require.EqualValues(t, 42, reopened.PromptTokens)
	require.EqualValues(t, 3, reopened.MessageCount)
	saved, err := messages.List(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, saved, 3)
	require.Equal(t, "one", saved[0].Content().String(), "history keeps its order")

	require.NoError(t, sessions.Delete(ctx, created.ID))
	left, err := messages.List(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, left, "deleting a conversation removes its messages")
	_, err = messages.Get(ctx, saved[0].ID)
	require.Error(t, err)
}

func TestConcurrentWritersShareTheDatabaseWithoutBusyErrors(t *testing.T) {
	connection := openStore(t)()
	defer connection.Close()
	ctx := context.Background()
	sessions, messages := NewService(db.New(connection)), message.NewService(db.New(connection))
	created, err := sessions.Create(ctx, "Busy")
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := range 40 { // a streaming reply, a title job and the UI all write at once
		wg.Go(func() {
			_, err := messages.Create(ctx, created.ID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprint(i)}}})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	all, err := messages.List(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, all, 40)
}
