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
	require.NoError(t, os.WriteFile(global, []byte(`{"future":{"keep":true},"tui":{"sidebar":"hidden"}}`), 0600))
	viper.SetConfigFile(global)
	cfg = &Config{WorkingDir: project, TUI: TUIConfig{Sidebar: "shown"}, Runtime: (RuntimeConfig{}).Defaults()}
	t.Cleanup(func() { cfg = old; viper.SetConfigFile(oldPath) })
	return global, ProjectConfigPath(project)
}
func TestUserSettingUpdatesOwningLayerAndResetRevealsInheritedValue(t *testing.T) {
	global, project := experienceConfig(t)
	require.NoError(t, os.WriteFile(project, []byte(`{"tui":{"sidebar":"shown"},"unknown":1}`), 0600))
	require.Equal(t, "project", EffectiveSetting("tui.sidebar").Origin)
	require.NoError(t, SaveUserSetting("tui.sidebar", "auto", false))
	require.Equal(t, "auto", cfg.TUI.Sidebar)
	require.NoError(t, SaveUserSetting("tui.sidebar", "", true))
	require.Equal(t, "hidden", cfg.TUI.Sidebar)
	require.Equal(t, "global", EffectiveSetting("tui.sidebar").Origin)
	data, err := os.ReadFile(project)
	require.NoError(t, err)
	require.Contains(t, string(data), `"unknown": 1`)
	require.NoError(t, SaveUserSetting("tui.sidebar", "", true))
	require.Equal(t, "hidden", cfg.TUI.Sidebar)
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
	require.Equal(t, "alt+k", Shortcut("commands"))
	require.True(t, strings.Contains(string(after), "future"))
}
func TestSidebarPreferenceRespectsTerminalWidth(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		width   int
		visible bool
	}{{"auto", 80, false}, {"auto", 139, false}, {"auto", 140, true}, {"shown", 109, false}, {"shown", 110, true}, {"hidden", 200, false}} {
		require.Equal(t, tc.visible, (TUIConfig{Sidebar: tc.mode}).SidebarVisible(tc.width))
	}
}

func TestSavingPreferenceRejectsMalformedOversizedAndSymlinkDocuments(t *testing.T) {
	global, project := experienceConfig(t)
	for _, data := range []string{`{"future":`, strings.Repeat(" ", maxSettingsDocument+1), `[]`, `null`} {
		require.NoError(t, os.WriteFile(global, []byte(data), 0600))
		require.Error(t, SaveUserSetting("tui.sidebar", "auto", false))
		after, err := os.ReadFile(global)
		require.NoError(t, err)
		require.Equal(t, data, string(after))
	}
	require.NoError(t, os.WriteFile(global, []byte(`{"future":900719925474099312345}`), 0600))
	require.NoError(t, SaveUserSetting("tui.sidebar", "auto", false))
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
