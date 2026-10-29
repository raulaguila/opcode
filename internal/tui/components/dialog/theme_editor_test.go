package dialog

import (
	"encoding/json"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func editableTheme(t *testing.T) *themeDialogCmp {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	settingsDialog(t)
	original := theme.CurrentThemeName()
	t.Cleanup(func() { _ = theme.DiscardCustomPreview(original) })
	require.NoError(t, theme.PreviewTheme("opcode"))
	m := NewThemeDialogCmp().(*themeDialogCmp)
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	require.NotNil(t, m.editor)
	return m
}
func TestThemeEditorCancelPreservesPaletteAndConfiguration(t *testing.T) {
	m := editableTheme(t)
	before := config.Get().TUI.Theme
	m.editor.fields[1].SetValue("#ffb283")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Equal(t, "#ffb283", theme.CurrentTheme().Primary().Dark)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, cmd)
	require.True(t, cmd().(ThemeChangedMsg).Preview)
	require.Nil(t, m.editor)
	require.Equal(t, "opcode", theme.CurrentThemeName())
	require.Equal(t, before, config.Get().TUI.Theme)
	path, err := theme.CustomThemePath()
	require.NoError(t, err)
	require.NoFileExists(t, path)
}
func TestThemeEditorExplicitSaveReloadAndInvalidInput(t *testing.T) {
	m := editableTheme(t)
	m.editor.fields[1].SetValue("bad")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	require.Nil(t, cmd)
	require.NotNil(t, m.editor)
	path, err := theme.CustomThemePath()
	require.NoError(t, err)
	require.NoFileExists(t, path)
	m.editor.fields[1].SetValue("#ffb283")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd)
	require.Equal(t, theme.CustomThemeName, cmd().(ThemeChangedMsg).ThemeName)
	require.Nil(t, m.editor)
	require.Equal(t, theme.CustomThemeName, config.Get().TUI.Theme)
	require.FileExists(t, path)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "#ffb283")
	require.NoError(t, theme.LoadCustomTheme())
	p, err := theme.PaletteFor(theme.CustomThemeName)
	require.NoError(t, err)
	require.Equal(t, "#ffb283", p.Primary.Dark)
}
func TestThemeEditorCompactResizeAndNoColorKeepFocus(t *testing.T) {
	m := editableTheme(t)
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(before)
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 12}, {Width: 60, Height: 20}, {Width: 80, Height: 24}} {
		m.Update(size)
		for i := 0; i < 8; i++ {
			view := m.View()
			require.LessOrEqual(t, lipgloss.Width(view), size.Width)
			require.LessOrEqual(t, lipgloss.Height(view), size.Height)
			require.Contains(t, ansi.Strip(view), "›")
			m.Update(tea.KeyMsg{Type: tea.KeyTab})
		}
	}
	m.editor.focus = 5
	for i := range m.editor.fields {
		m.editor.fields[i].Blur()
	}
	m.editor.fields[5].Focus()
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	require.Equal(t, 5, m.editor.focus)
	require.True(t, m.editor.fields[5].Focused())
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
}

func TestThemeEditorFailedSettingsSaveRestoresDiskAndRegistry(t *testing.T) {
	for _, existed := range []bool{true, false} {
		name := "new_palette"
		if existed {
			name = "active_custom_palette"
		}
		t.Run(name, func(t *testing.T) {
			m := editableTheme(t)
			cfg := config.Get()
			original, err := theme.PaletteFor("opcode")
			require.NoError(t, err)
			path, err := theme.CustomThemePath()
			require.NoError(t, err)
			var originalBytes []byte
			if existed {
				require.NoError(t, theme.SaveCustomPalette(original))
				m.editor.restore = theme.CustomThemeName
				m.currentTheme = theme.CustomThemeName
				// Preserve user formatting exactly when rolling back.
				data, err := json.Marshal(original)
				require.NoError(t, err)
				originalBytes = append([]byte(" \n"), append(data, '\n', '\n')...)
				require.NoError(t, localstate.WriteBytes(path, originalBytes))
			}
			registeredBefore := theme.GetTheme(theme.CustomThemeName)
			configuredBefore := cfg.TUI.Theme
			m.editor.fields[1].SetValue("#ffb283")
			m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
			// Invalid JSON fails the config patch without relying on host file
			// permissions (which behave differently when tests run as root).
			configPath := viper.ConfigFileUsed()
			require.NoError(t, os.WriteFile(configPath, []byte(`{`), 0600))
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
			require.Nil(t, cmd)
			require.NotNil(t, m.editor)
			require.Contains(t, m.editor.note, "previous palette restored")
			require.Equal(t, configuredBefore, cfg.TUI.Theme)
			if registeredBefore == nil {
				require.Nil(t, theme.GetTheme(theme.CustomThemeName))
			} else {
				require.Same(t, registeredBefore, theme.GetTheme(theme.CustomThemeName))
			}
			if existed {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, originalBytes, data)
			} else {
				require.NoFileExists(t, path)
			}
			m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			require.Nil(t, m.editor)
			require.Equal(t, m.currentTheme, theme.CurrentThemeName())
			require.Equal(t, original.Primary, theme.CurrentTheme().Primary())
			require.NoError(t, theme.LoadCustomTheme())
			if existed {
				loaded, err := theme.PaletteFor(theme.CustomThemeName)
				require.NoError(t, err)
				require.Equal(t, original, loaded, "restart must keep the last successful palette")
			}
			configBytes, err := os.ReadFile(configPath)
			require.NoError(t, err)
			require.Equal(t, []byte(`{`), configBytes)
		})
	}
}
