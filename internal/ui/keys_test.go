package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestConfiguredShortcutsDriveTheScreenAndItsLabels(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(settings)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	cfg, err := config.Load(root, false) // cached once per binary: set the keys on it
	require.NoError(t, err)
	old := cfg.TUI.Keybindings
	cfg.TUI.Keybindings = map[string]string{"commands": "alt+k"}
	t.Cleanup(func() { cfg.TUI.Keybindings = old })

	m := New(nil)
	m.width, m.height = 100, 30
	m.key(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	require.Nil(t, m.dialog, "the old default no longer opens the palette")
	m.key(tea.KeyPressMsg{Code: 'k', Mod: tea.ModAlt})
	require.NotNil(t, m.dialog, "the configured key opens it")

	m.dialog = nil
	m.key(tea.KeyPressMsg{Code: tea.KeyF1})
	help := ansi.Strip(m.dialog.view(m))
	require.Contains(t, help, "Alt+K", "help shows the configured key")
	require.NotContains(t, help, "Ctrl+K")
}

func TestShortcutAndNumericSettingsReadInTheScreensWords(t *testing.T) {
	setting := func(path string) config.Setting {
		for _, s := range config.UserSettings {
			if s.Path == path {
				return s
			}
		}
		t.Fatalf("no setting %s", path)
		return config.Setting{}
	}
	require.Equal(t, "Ctrl+K", settingValue(setting("tui.keybindings.commands"), "ctrl+k"))
	require.Equal(t, "Alt+B", keyLabel("alt+b"))
	require.Equal(t, "Full width", settingValue(setting("tui.proseWidth"), "0"))
	require.Equal(t, "100 columns", settingValue(setting("tui.proseWidth"), "100"))
	require.Equal(t, "30 min", settingValue(setting("runtime.maxSeconds"), "1800"))
	require.Equal(t, "90 s", settingValue(setting("runtime.idleTimeout"), "90"))
	require.Equal(t, "375 KiB", settingValue(setting("runtime.toolBudget"), "384000"))
	require.Equal(t, "Off", settingValue(setting("runtime.embeddingModel"), ""))
	require.Equal(t, "Commands", localLabel("commands"))
}

func TestFailureCardAlwaysSaysWhatStopped(t *testing.T) {
	limit := "failed to process events: model output token limit reached; incomplete call rejected"
	require.Equal(t, "The reply hit the output token limit", failureTitle(limit))
	require.Contains(t, explain(limit), "output tokens")
	require.Equal(t, "disk quota exceeded.", explain("disk quota exceeded"), "an unknown error is shown, not hidden")
	require.Empty(t, explain("context canceled"), "a titled cause needs no repeat")
}
