package config

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurationMigrationPreservesPrivateBackupUnknownFieldsAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte("{\n\"future\":9007199254740993,\"providers\":{\"openai\":{\"apiKey\":\"private-migration-key\"}}}\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	backup, changed, err := MigrateDocument(path)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, filepath.ToSlash(backup), "/.opcode/config-backups/")
	saved, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, original, saved)
	info, err := os.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	next, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(next), "9007199254740993")
	require.Contains(t, string(next), `"schemaVersion": 1`)
	_, changed, err = MigrateDocument(path)
	require.NoError(t, err)
	require.False(t, changed)
	// Restoring the backup is the complete rollback; old formats keep loading.
	require.NoError(t, os.WriteFile(path, saved, 0600))
	require.NoError(t, ValidateDocument(path))
}
func TestValidateConfigurationRejectsTypedFieldsFutureVersionsAndSymlinksWithoutPayloads(t *testing.T) {
	for _, doc := range []string{`{"schemaVersion":2}`, `{"runtime":{"maxSteps":"private-value"}}`, `{"tui":{"language":"invalid"}}`, `{"providerProfiles":{"local":{"backend":"local","api":"unsupported"}}}`} {
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(doc), 0600))
		err := ValidateDocument(path)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-value")
		_, _, err = MigrateDocument(path)
		require.Error(t, err)
		files, _ := os.ReadDir(filepath.Dir(path))
		require.Len(t, files, 1)
	}
	target := filepath.Join(t.TempDir(), "secret.json")
	require.NoError(t, os.WriteFile(target, []byte(`{}`), 0600))
	link := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.Symlink(target, link))
	require.Error(t, ValidateDocument(link))
	_, _, err := MigrateDocument(link)
	require.Error(t, err)
}
func TestRedactedConfigurationDoesNotExportUnknownValuesURLsOrLiteralCredentials(t *testing.T) {
	isolateMigrationConfig(t)
	path := filepath.Join(os.Getenv("HOME"), ".opcode", "config.json")
	writeMigrationJSON(t, path, `{"future":"private-unknown", "mcpServers":{"x":{"type":"http","url":"https://example.test/?secret=private-query","headers":{"Authorization":"private-header"}}},"providers":{"local":{"baseURL":"http://127.0.0.1:11434/v1","apiKey":"private-literal"}}}`)
	_, err := LoadOffline(t.TempDir())
	require.NoError(t, err)
	raw, err := json.Marshal(RedactedDocument())
	require.NoError(t, err)
	for _, secret := range []string{"private-unknown", "private-query", "private-header", "private-literal"} {
		require.NotContains(t, string(raw), secret)
	}
}
