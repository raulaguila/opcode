package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func experienceConfig(t *testing.T) (string, string) {
	t.Helper()
	old := cfg
	oldPath := viper.ConfigFileUsed()
	root := t.TempDir()
	global := filepath.Join(root, "global.json")
	project := filepath.Join(root, "project")
	require.NoError(t, os.Mkdir(project, 0700))
	require.NoError(t, os.Mkdir(filepath.Join(project, ".opcode"), 0700))
	require.NoError(t, os.WriteFile(global, []byte(`{"future":{"keep":true},"tui":{"notifications":"bell"}}`), 0600))
	viper.SetConfigFile(global)
	cfg = &Config{WorkingDir: project, TUI: TUIConfig{Notifications: "auto"}, Runtime: (RuntimeConfig{}).Defaults()}
	t.Cleanup(func() { cfg = old; viper.SetConfigFile(oldPath) })
	return global, ProjectConfigPath(project)
}
func TestUserSettingUpdatesOwningLayerAndResetRevealsInheritedValue(t *testing.T) {
	global, project := experienceConfig(t)
	require.NoError(t, os.WriteFile(project, []byte(`{"tui":{"notifications":"auto"},"unknown":1}`), 0600))
	require.Equal(t, "project", EffectiveSetting("tui.notifications").Origin)
	require.NoError(t, SaveUserSetting("tui.notifications", "osc", false))
	require.Equal(t, "osc", cfg.TUI.Notifications)
	require.NoError(t, SaveUserSetting("tui.notifications", "", true))
	require.Equal(t, "bell", cfg.TUI.Notifications)
	require.Equal(t, "global", EffectiveSetting("tui.notifications").Origin)
	data, err := os.ReadFile(project)
	require.NoError(t, err)
	require.Contains(t, string(data), `"unknown": 1`)
	require.NoError(t, SaveUserSetting("tui.notifications", "", true))
	require.Equal(t, "off", cfg.TUI.Notifications)
	data, err = os.ReadFile(global)
	require.NoError(t, err)
	require.Contains(t, string(data), "future")
	info, err := os.Stat(global)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
func TestEffectiveConfigurationNeverExposesCredentialOrURLSecrets(t *testing.T) {
	experienceConfig(t)
	t.Setenv("EXPERIENCE_KEY", "secret-value")
	cfg.ProviderProfiles = map[string]ProviderProfile{"studio": {Backend: models.ProviderLocal, Provider: Provider{APIKey: "secret-value", APIKeyEnv: "EXPERIENCE_KEY", BaseURL: "https://user:password@example.com/v1?token=query-secret"}}}
	cfg.MCPServers = map[string]MCPServer{"private": {Headers: map[string]string{"Authorization": "Bearer private-bearer"}, Env: []string{"SECRET=secret-child"}}}
	raw, _ := json.Marshal(EffectiveConfiguration())
	for _, secret := range []string{"secret-value", "password", "query-secret", "private-bearer", "secret-child"} {
		require.NotContains(t, string(raw), secret)
	}
	require.Contains(t, string(raw), "environment (EXPERIENCE_KEY)")
	require.Contains(t, string(raw), "redacted")
}
func TestSettingValidationDoesNotWriteInvalidShortcutsOrRuntime(t *testing.T) {
	global, _ := experienceConfig(t)
	before, _ := os.ReadFile(global)
	for _, tc := range [][2]string{{"runtime.maxSteps", "999"}, {"runtime.maxSteps", "12 junk"}, {"tui.keybindings.commands", "ctrl+c"}, {"tui.keybindings.commands", "ctrl+p"}, {"tui.language", "xx"}, {"security.sandbox", "none"}} {
		require.Error(t, SaveUserSetting(tc[0], tc[1], false))
	}
	after, _ := os.ReadFile(global)
	require.Equal(t, string(before), string(after))
	require.NoError(t, SaveUserSetting("tui.keybindings.commands", "alt+k", false))
	require.True(t, strings.Contains(string(after), "future"))
}
func TestInterfaceLanguageFollowsLocaleUntilChosen(t *testing.T) {
	for env, want := range map[string]string{"pt_BR.UTF-8": "pt", "pt": "pt", "en_US.UTF-8": "en", "": "en", "C": "en"} {
		require.Equal(t, want, localeLanguage(func(name string) string {
			if name == "LANG" {
				return env
			}
			return ""
		}), env)
	}
	// LC_ALL wins over LANG, as in libc.
	require.Equal(t, "en", localeLanguage(func(name string) string { return map[string]string{"LC_ALL": "C", "LANG": "pt_BR"}[name] }))
}

func TestRetiredShortcutsStillLoad(t *testing.T) {
	require.NoError(t, TUIConfig{Keybindings: map[string]string{"sidebar": "alt+b", "theme": "ctrl+t"}}.Validate())
	require.Error(t, TUIConfig{Keybindings: map[string]string{"nonsense": "alt+q"}}.Validate())
}

func TestSavingPreferenceRejectsMalformedOversizedAndSymlinkDocuments(t *testing.T) {
	global, project := experienceConfig(t)
	for _, data := range []string{`{"future":`, strings.Repeat(" ", maxSettingsDocument+1), `[]`, `null`} {
		require.NoError(t, os.WriteFile(global, []byte(data), 0600))
		require.Error(t, SaveUserSetting("tui.notifications", "bell", false))
		after, err := os.ReadFile(global)
		require.NoError(t, err)
		require.Equal(t, data, string(after))
	}
	require.NoError(t, os.WriteFile(global, []byte(`{"future":900719925474099312345}`), 0600))
	require.NoError(t, SaveUserSetting("tui.notifications", "bell", false))
	after, err := os.ReadFile(global)
	require.NoError(t, err)
	require.Contains(t, string(after), "900719925474099312345")
	require.NoError(t, os.Symlink(global, project))
	require.Error(t, SaveUserSetting("tui.language", "pt", false))
	require.NoError(t, os.Remove(project))
	require.NoError(t, os.WriteFile(project, []byte(`{"broken":`), 0600))
	require.Error(t, SaveUserSetting("tui.language", "pt", false))
}
func TestEffectiveOriginAttributesEnvironmentAliasesAndRefusesMisleadingEdit(t *testing.T) {
	experienceConfig(t)
	t.Setenv("OPCODE_TUI_LANGUAGE", "pt")
	cfg.TUI.Language = "pt"
	require.Equal(t, "environment (OPCODE_TUI_LANGUAGE)", EffectiveSetting("tui.language").Origin)
	require.ErrorContains(t, SaveUserSetting("tui.language", "en", false), "OPCODE_TUI_LANGUAGE")
	t.Setenv("OPENCODE_RUNTIME_MAXSTEPS", "12")
	cfg.Runtime.MaxSteps = 12
	require.Equal(t, "environment (OPENCODE_RUNTIME_MAXSTEPS)", EffectiveSetting("runtime.maxSteps").Origin)
}

func TestSemanticSearchPreferenceTurnsLocalEmbeddingsOnAndOff(t *testing.T) {
	experienceConfig(t)
	require.Nil(t, cfg.Embeddings())
	require.NoError(t, SaveUserSetting("runtime.embeddingModel", "nomic-embed-text", false))
	require.Equal(t, &EmbeddingConfig{DefaultEmbeddingEndpoint, "nomic-embed-text"}, cfg.Embeddings())
	require.NoError(t, cfg.Embeddings().Validate())
	require.NoError(t, SaveUserSetting("runtime.embeddingModel", "", false))
	require.Nil(t, cfg.Embeddings())
	require.Error(t, SaveUserSetting("runtime.embeddingModel", strings.Repeat("m", 200), false))
	cfg.Index.Embeddings = &EmbeddingConfig{"http://127.0.0.1:9000/api/embed", "explicit"}
	cfg.Runtime.EmbeddingModel = "nomic-embed-text"
	require.Equal(t, "explicit", cfg.Embeddings().Model, "index.embeddings stays the explicit choice")
}

func TestProjectFileCannotTurnSemanticSearchOn(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".opcode.json"), []byte(`{"runtime":{"embeddingModel":"nomic-embed-text"}}`), 0600))
	loaded, err := Load(root, false)
	require.NoError(t, err)
	require.Nil(t, loaded.Embeddings())
}
