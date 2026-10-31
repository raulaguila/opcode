//go:build linux || darwin || freebsd || openbsd || netbsd

package dialog

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/commands"
	"github.com/stretchr/testify/require"
)

// Run against an isolated clipboard executable before clipboard package init.
// Never reads or writes the user's clipboard.
func TestAsyncClipboardReachesPaletteAndSettings(t *testing.T) {
	if os.Getenv("OPCODE_TEST_CLIPBOARD_HELPER") != "1" {
		directory := t.TempDir()
		for _, name := range []string{"xclip", "xsel", "wl-copy", "wl-paste", "pbpaste", "pbcopy"} {
			require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\nprintf '%s' permissions\n"), 0700))
		}
		child := exec.Command(os.Args[0], "-test.run=^TestAsyncClipboardReachesPaletteAndSettings$")
		child.Env = append(os.Environ(), "OPCODE_TEST_CLIPBOARD_HELPER=1", "PATH="+directory, "WAYLAND_DISPLAY=isolated-test")
		output, err := child.CombinedOutput()
		require.NoError(t, err, string(output))
		return
	}
	settings := settingsDialog(t)
	palette := NewCommandDialogCmp().(*commandDialogCmp)
	var entries []Command
	for _, c := range commands.Builtins {
		entries = append(entries, Command{ID: c.ID, Title: c.Title, Description: c.Description})
	}
	palette.SetCommands(entries)
	_, paste := palette.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	require.NotNil(t, paste)
	palette.Update(paste())
	require.Equal(t, "permissions", palette.search.Value())
	require.Equal(t, "permissions", palette.listView.GetItems()[0].ID)
	settings.settings()
	_, paste = settings.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	require.NotNil(t, paste)
	settings.Update(paste())
	require.Equal(t, "permissions", settings.settingsFilter.Value())
	// Non-enumerated setting editing has its own focused text input.
	settings.screen = "setting"
	settings.settingPath = "tui.language"
	settings.setFields([]string{"Text"}, []string{""})
	settings.settingPath = "tui.keybindings.models" // An unrestricted text field, not an enum.
	_, paste = settings.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	require.NotNil(t, paste)
	settings.Update(paste())
	require.Equal(t, "permissions", settings.fields[0].Value())
}
