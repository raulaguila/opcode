package config

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func isolateMigrationConfig(t *testing.T) {
	t.Helper()
	original := cfg
	originalViper := *viper.GetViper()
	registry := maps.Clone(models.SupportedModels)
	cfg = nil
	viper.Reset()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("OPCODE_DEV_DEBUG", "")
	t.Setenv("OPENCODE_DEV_DEBUG", "")
	t.Cleanup(func() { cfg = original; *viper.GetViper() = originalViper; models.SupportedModels = registry })
}
func writeMigrationJSON(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
}
func TestLegacyGlobalAndProjectLoadInPlaceWithOldDataAndTheme(t *testing.T) {
	isolateMigrationConfig(t)
	home := os.Getenv("HOME")
	root := t.TempDir()
	global := filepath.Join(home, ".opencode.json")
	project := filepath.Join(root, ".opencode.json")
	oldData := filepath.Join(root, ".opencode")
	require.NoError(t, os.Mkdir(oldData, 0700))
	t.Setenv("MIGRATION_PROVIDER_KEY", "private-test-environment")
	writeMigrationJSON(t, global, `{"providers":{"openai":{"apiKeyEnv":"MIGRATION_PROVIDER_KEY"}},"tui":{"theme":"opencode"},"security":{"sandbox":"none"},"future":{"keep":true}}`)
	writeMigrationJSON(t, project, `{"runtime":{"mode":"ask"},"security":{"sandbox":"bubblewrap"},"extensions":{"hooks":[{"event":"before_tool","command":"must never activate"}]}}`)
	loaded, err := Load(root, false)
	require.NoError(t, err)
	require.Equal(t, global, viper.ConfigFileUsed())
	require.Equal(t, oldData, loaded.Data.Directory)
	require.Equal(t, "opcode", loaded.TUI.Theme)
	require.Equal(t, "ask", loaded.Runtime.Mode)
	require.Equal(t, "none", loaded.Security.Sandbox)
	require.Empty(t, loaded.Extensions.Hooks)
	require.Equal(t, project, ProjectConfigPath(root))
	require.Equal(t, "private-test-environment", loaded.Providers[models.ProviderOpenAI].APIKey)
	require.NoError(t, SavePreferences(Preferences{ResumeLast: true}, loaded.Runtime))
	updated, err := os.ReadFile(global)
	require.NoError(t, err)
	require.NotContains(t, string(updated), "private-test-environment")
	require.Contains(t, string(updated), "MIGRATION_PROVIDER_KEY")
	require.Contains(t, string(updated), `"future"`)
	require.Contains(t, string(updated), `"opencode"`, "theme alias is adapted only in memory")
	_, err = os.Stat(filepath.Join(home, ".opcode.json"))
	require.True(t, os.IsNotExist(err), "legacy credentials must not be copied to another file")
}
func TestNewConfigAndDataTakePrecedenceAcrossLocations(t *testing.T) {
	isolateMigrationConfig(t)
	home := os.Getenv("HOME")
	root := t.TempDir()
	xdg := os.Getenv("XDG_CONFIG_HOME")
	writeMigrationJSON(t, filepath.Join(home, ".opencode.json"), `{"runtime":{"mode":"execute"}}`)
	currentGlobal := filepath.Join(xdg, "opcode", ".opcode.json")
	writeMigrationJSON(t, currentGlobal, `{"runtime":{"mode":"plan"}}`)
	writeMigrationJSON(t, filepath.Join(root, ".opencode.json"), `{"runtime":{"mode":"execute"}}`)
	writeMigrationJSON(t, filepath.Join(root, ".opcode.json"), `{"runtime":{"mode":"ask"}}`)
	require.NoError(t, os.Mkdir(filepath.Join(root, ".opencode"), 0700))
	require.NoError(t, os.Mkdir(filepath.Join(root, ".opcode"), 0700))
	loaded, err := Load(root, false)
	require.NoError(t, err)
	require.Equal(t, currentGlobal, viper.ConfigFileUsed())
	require.Equal(t, "ask", loaded.Runtime.Mode)
	require.Equal(t, filepath.Join(root, ".opcode"), loaded.Data.Directory)
}
func TestEnvironmentAliasesPreferNewIncludingExplicitEmpty(t *testing.T) {
	old := map[string]string{"OPENCODE_DEV_DEBUG": "true", "OPENCODE_TEST_LOCAL_MODEL": "legacy"}
	lookup := func(name string) (string, bool) { v, ok := old[name]; return v, ok }
	value, ok := LookupEnvironmentWith("OPCODE_DEV_DEBUG", lookup)
	require.True(t, ok)
	require.Equal(t, "true", value)
	old["OPCODE_DEV_DEBUG"] = ""
	value, ok = LookupEnvironmentWith("OPCODE_DEV_DEBUG", lookup)
	require.True(t, ok)
	require.Empty(t, value)
	old["OPCODE_DEV_DEBUG"] = "false"
	value, ok = LookupEnvironmentWith("OPENCODE_DEV_DEBUG", lookup)
	require.True(t, ok)
	require.Equal(t, "false", value)
	old["OPENAI_API_KEY"] = "provider"
	value, ok = LookupEnvironmentWith("OPENAI_API_KEY", lookup)
	require.True(t, ok)
	require.Equal(t, "provider", value)
}
func TestLegacyEnvironmentConfigAndExplicitDataDirectory(t *testing.T) {
	isolateMigrationConfig(t)
	root := t.TempDir()
	explicit := filepath.Join(t.TempDir(), "private-data")
	writeMigrationJSON(t, filepath.Join(root, ".opencode.json"), `{"data":{"directory":"relative-private"}}`)
	t.Setenv("OPENCODE_RUNTIME_MODE", "plan")
	t.Setenv("OPCODE_RUNTIME_MODE", "ask")
	t.Setenv("OPENCODE_DATA_DIRECTORY", explicit)
	loaded, err := Load(root, false)
	require.NoError(t, err)
	require.Equal(t, "ask", loaded.Runtime.Mode)
	require.Equal(t, explicit, loaded.Data.Directory)
}
func TestLegacyProjectChangesSaveBackWithoutCreatingNewCredentialFile(t *testing.T) {
	isolateMigrationConfig(t)
	root := t.TempDir()
	global := filepath.Join(os.Getenv("HOME"), ".opcode.json")
	writeMigrationJSON(t, global, `{}`)
	project := filepath.Join(root, ".opencode.json")
	writeMigrationJSON(t, project, `{"providers":{"openai":{"apiKey":"literal-private-fixture"}},"future":{"keep":9007199254740993}}`)
	_, err := Load(root, false)
	require.NoError(t, err)
	require.NoError(t, SaveProvider(models.ProviderOpenAI, Provider{APIKey: "literal-private-fixture", BaseURL: "https://example.invalid/v1"}))
	raw, err := os.ReadFile(project)
	require.NoError(t, err)
	require.Contains(t, string(raw), "9007199254740993")
	require.Contains(t, string(raw), "example.invalid")
	_, err = os.Stat(filepath.Join(root, ".opcode.json"))
	require.True(t, os.IsNotExist(err))
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
}
func TestLegacyGuidanceAndDatabasePathsHaveSafePrecedence(t *testing.T) {
	root := t.TempDir()
	require.Contains(t, defaultGuidancePaths(root), "OpenCode.md")
	writeMigrationJSON(t, filepath.Join(root, "Opcode.md"), "new guidance")
	require.NotContains(t, defaultGuidancePaths(root), "OpenCode.md")
	require.Equal(t, filepath.Join(root, "opcode.db"), DatabasePath(root))
	require.NoError(t, os.WriteFile(filepath.Join(root, "opencode.db"), nil, 0600))
	require.Equal(t, filepath.Join(root, "opencode.db"), DatabasePath(root))
	require.NoError(t, os.WriteFile(filepath.Join(root, "opcode.db"), nil, 0600))
	require.Equal(t, filepath.Join(root, "opcode.db"), DatabasePath(root))
	for _, path := range []string{"/project/.opencode/opencode.db", "/project/.opencode.json", "/project/opencode.json", "/project/.opencode-integration.json"} {
		require.True(t, LegacySensitivePath(path))
	}
	require.False(t, LegacySensitivePath("/project/opencode.go"))
}
func TestMalformedLegacyProjectDoesNotSilentlyFallBack(t *testing.T) {
	isolateMigrationConfig(t)
	root := t.TempDir()
	writeMigrationJSON(t, filepath.Join(root, ".opencode.json"), "invalid-json")
	_, err := Load(root, false)
	require.ErrorContains(t, err, "project configuration")
}

func TestExplicitEmptyNewEnvironmentNeverFallsBackToLegacyValue(t *testing.T) {
	isolateMigrationConfig(t)
	root := t.TempDir()
	t.Setenv("OPENCODE_RUNTIME_MODE", "plan")
	t.Setenv("OPCODE_RUNTIME_MODE", "")
	loaded, err := Load(root, false)
	require.NoError(t, err)
	require.Equal(t, "", viper.GetString("runtime.mode"))
	require.Equal(t, "execute", loaded.Runtime.Mode)
}

func TestGuidanceMigrationPreservesBaseWhenOnlyLocalNewFileExists(t *testing.T) {
	root := t.TempDir()
	writeMigrationJSON(t, filepath.Join(root, "Opcode.local.md"), "local guidance")
	paths := defaultGuidancePaths(root)
	require.Contains(t, paths, "OpenCode.md")
	require.NotContains(t, paths, "OpenCode.local.md")
}
