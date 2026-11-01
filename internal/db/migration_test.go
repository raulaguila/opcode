package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestConnectUsesExistingLegacyDatabaseAndItsLiveWALWithoutCopying(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, ".opencode")
	require.NoError(t, os.Mkdir(data, 0700))
	legacy := filepath.Join(data, "opencode.db")
	writer, err := sql.Open("sqlite3", legacy)
	require.NoError(t, err)
	defer writer.Close()
	_, err = writer.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = writer.Exec("PRAGMA wal_autocheckpoint=0")
	require.NoError(t, err)
	_, err = writer.Exec("CREATE TABLE migration_probe(value TEXT NOT NULL)")
	require.NoError(t, err)
	_, err = writer.Exec("INSERT INTO migration_probe VALUES ('committed-in-wal')")
	require.NoError(t, err)
	wal, err := os.Stat(legacy + "-wal")
	require.NoError(t, err)
	require.Positive(t, wal.Size())
	before, err := os.Stat(legacy)
	require.NoError(t, err)
	file := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(file, []byte(`{}`), 0600))
	viper.SetConfigFile(file)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	loaded, err := config.Load(root, false)
	require.NoError(t, err)
	require.Equal(t, data, loaded.Data.Directory)
	connection, err := Connect()
	require.NoError(t, err)
	defer connection.Close()
	var value string
	require.NoError(t, connection.QueryRow("SELECT value FROM migration_probe").Scan(&value))
	require.Equal(t, "committed-in-wal", value)
	after, err := os.Stat(legacy)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	_, err = os.Stat(filepath.Join(data, "opcode.db"))
	require.True(t, os.IsNotExist(err), "migration must not create a new database alongside the existing live database")
}

func TestConnectAppliesPragmasToEveryPooledConnection(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(file, []byte(`{}`), 0600))
	viper.SetConfigFile(file)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	_, err := config.Load(root, false)
	require.NoError(t, err)
	connection, err := Connect()
	require.NoError(t, err)
	defer connection.Close()

	// Hold several connections at once so the pool must open new ones.
	ctx := t.Context()
	for range 3 {
		conn, err := connection.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var foreignKeys int
		var journal string
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys))
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal))
		require.Equal(t, 1, foreignKeys)
		require.Equal(t, "wal", journal)
	}
}
