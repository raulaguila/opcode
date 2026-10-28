package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestResetGlobalBacksUpExactBytesAndPreservesProjectData(t *testing.T) {
	for _, name := range []string{"canonical", "legacy", "malformed"} {
		t.Run(name, func(t *testing.T) {
			isolateMigrationConfig(t)
			home, project := os.Getenv("HOME"), t.TempDir()
			path := filepath.Join(home, ".opcode", "config.json")
			source := path
			original := "{\n  \"providers\": {\"openai\": {\"apiKey\": \"private-reset-fixture\"}},\n  \"future\": 9007199254740993\n}\n"
			if name == "legacy" {
				source = filepath.Join(home, ".opencode.json")
			}
			if name == "malformed" {
				original = `{"secret":"private-reset-fixture","broken":`
			}
			writeMigrationJSON(t, source, original)
			projectConfig := filepath.Join(project, ".opcode", "config.json")
			database := filepath.Join(project, ".opcode", "opcode.db")
			writeMigrationJSON(t, projectConfig, `{"runtime":{"mode":"plan"}}`)
			writeMigrationJSON(t, database, "keep existing conversations")
			resetPath, backup, err := ResetGlobalConfig()
			require.NoError(t, err)
			require.Equal(t, path, resetPath)
			require.Equal(t, filepath.Join(home, ".opcode", "config-backups"), filepath.Dir(backup))
			raw, err := os.ReadFile(backup)
			require.NoError(t, err)
			require.Equal(t, original, string(raw), "backup must preserve even invalid JSON exactly")
			info, err := os.Stat(backup)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			raw, err = os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, defaultConfigTemplate, raw)
			require.NoError(t, ValidateDocument(path))
			loaded, err := LoadOffline(project)
			require.NoError(t, err)
			require.Equal(t, path, viper.ConfigFileUsed())
			require.Equal(t, "plan", loaded.Runtime.Mode, "project overrides still apply")
			require.Equal(t, "bubblewrap", loaded.Security.Sandbox)
			for file, want := range map[string]string{projectConfig: `{"runtime":{"mode":"plan"}}`, database: "keep existing conversations"} {
				raw, err := os.ReadFile(file)
				require.NoError(t, err)
				require.Equal(t, want, string(raw))
			}
			if source != path {
				raw, err := os.ReadFile(source)
				require.NoError(t, err)
				require.Equal(t, original, string(raw), "legacy original remains inactive")
			}
			// Restoring the private backup is sufficient for rollback.
			raw, err = os.ReadFile(backup)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, raw, 0600))
			_, nextBackup, err := ResetGlobalConfig()
			require.NoError(t, err)
			require.NotEqual(t, backup, nextBackup)
			raw, err = os.ReadFile(nextBackup)
			require.NoError(t, err)
			require.Equal(t, original, string(raw))
		})
	}
}

func TestResetWithoutExistingConfigCreatesPrivateDefaults(t *testing.T) {
	isolateMigrationConfig(t)
	path, backup, err := ResetGlobalConfig()
	require.NoError(t, err)
	require.Empty(t, backup)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, defaultConfigTemplate, raw)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestResetRefusesUnsafeSourcesAndBackupFailurePreservesOriginal(t *testing.T) {
	for _, name := range []string{"source symlink", "source directory", "oversized source", "backup symlink"} {
		t.Run(name, func(t *testing.T) {
			isolateMigrationConfig(t)
			path := filepath.Join(os.Getenv("HOME"), ".opcode", "config.json")
			outside := filepath.Join(t.TempDir(), "outside.json")
			original := `{"private":"keep this original"}`
			writeMigrationJSON(t, outside, original)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			switch name {
			case "source symlink":
				require.NoError(t, os.Symlink(outside, path))
			case "source directory":
				require.NoError(t, os.Mkdir(path, 0700))
			case "oversized source":
				require.NoError(t, os.WriteFile(path, make([]byte, maxSettingsDocument+1), 0600))
			case "backup symlink":
				writeMigrationJSON(t, path, original)
				require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(filepath.Dir(path), "config-backups")))
			}
			_, _, err := ResetGlobalConfig()
			require.Error(t, err)
			require.NotContains(t, err.Error(), "keep this original")
			raw, err := os.ReadFile(outside)
			require.NoError(t, err)
			require.Equal(t, original, string(raw))
			if name == "backup symlink" {
				raw, err = os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, original, string(raw))
			}
		})
	}
}
