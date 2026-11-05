package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/ui/dialog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestSettingOptionsReadChoicesButNotRanges(t *testing.T) {
	require.Equal(t, []string{"pt", "en"}, settingOptions(config.Setting{Help: "pt, en"}))
	require.Equal(t, []string{"off", "bell", "auto", "native", "osc"}, settingOptions(config.Setting{Help: "off, bell, auto, native, osc (only when unfocused)"}))
	require.Nil(t, settingOptions(config.Setting{Help: "1–200"}))
	require.Nil(t, settingOptions(config.Setting{Help: "ctrl+letter, alt+letter"}))
	require.Equal(t, "Language", localLabel("Idioma / Language"))
}

func TestConnectionViewDrawsFieldsMaskedAndTheCatalogResult(t *testing.T) {
	view := ansi.Strip(connectionView(dialog.ProviderFormState{
		Title: "Nova conexão", Meta: "Ollama", Tested: true, Note: "8 modelos",
		Fields: []dialog.ProviderFormField{
			{Label: "Nome", Value: "Ollama", Cursor: 6, Focused: true},
			{Label: "Chave", Value: "sk-secret", Secret: true},
			{Label: "Auth", Value: "none", Options: []string{"none", "api_key"}, OptionLabels: []string{"Nenhuma", "Chave"}},
		},
	}, 100))
	require.Contains(t, view, " Nome   Ollama▏", "labels sit right-aligned")
	require.NotContains(t, view, "sk-secret")
	require.Contains(t, view, " Auth   ◉ Nenhuma   ○ Chave")
	require.Contains(t, view, "✓ 8 modelos")
}

func TestConnectionsShowKeysInsteadOfButtons(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	global := filepath.Join(t.TempDir(), "global.json")
	require.NoError(t, os.WriteFile(global, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(global)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	_, err := config.Load(root, false)
	require.NoError(t, err)
	useWorkspace(t, root)
	cfg := config.Get()
	oldProfiles := cfg.ProviderProfiles
	t.Cleanup(func() { cfg.ProviderProfiles = oldProfiles })
	cfg.ProviderProfiles = map[string]config.ProviderProfile{"casa": {Name: "Casa"}}
	for _, width := range []int{80, 120} {
		m := New(nil)
		m.width, m.height = width, 30
		m.openSettingsAt(sectionConnections)
		view := ansi.Strip(m.dialog.(*settingsPanel).view(m))
		require.Contains(t, view, "Casa")
		require.NotContains(t, view, "▕ Edit")
		require.NotContains(t, view, "+ New connection")
		require.NotContains(t, view, "+ Nova conexão")
		for _, key := range []string{"N ", "Enter ", "T ", "M ", "A ", "D ", "Esc "} {
			require.Contains(t, view, "  "+key, "width %d", width)
		}
	}
}

func TestFieldCaretFollowsTheCursor(t *testing.T) {
	draw := func(field dialog.ProviderFormField) string {
		return strings.TrimRight(ansi.Strip(fieldValue(field, 12)), " ")
	}
	require.Equal(t, " ab▏cd", draw(dialog.ProviderFormField{Value: "abcd", Cursor: 2, Focused: true}))
	require.Equal(t, " abcd", draw(dialog.ProviderFormField{Value: "abcd", Cursor: 2}))
	require.Equal(t, " …ijklmnop▏", draw(dialog.ProviderFormField{Value: "abcdefghijklmnop", Cursor: 16, Focused: true}))
	require.Equal(t, " ▏abcdefghi", draw(dialog.ProviderFormField{Value: "abcdefghijklmnop", Cursor: 0, Focused: true}))
	require.Equal(t, " ••▏•", draw(dialog.ProviderFormField{Value: "abc", Cursor: 2, Focused: true, Secret: true}))
	require.Equal(t, " ▏hint", draw(dialog.ProviderFormField{Placeholder: "hint", Focused: true}))
}

func TestPresetNameCanBeErasedAndTypedOver(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	global := filepath.Join(t.TempDir(), "global.json")
	require.NoError(t, os.WriteFile(global, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(global)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	_, err := config.Load(root, false)
	require.NoError(t, err)
	useWorkspace(t, root)
	for preset := range config.ProviderPresets {
		m := New(nil)
		m.width, m.height = 110, 30
		m.openProviderForm("")
		form := m.dialog.(*hosted).cmp.(formStater)
		for range preset {
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		state, ok := form.FormState()
		require.True(t, ok)
		for range len([]rune(state.Fields[0].Value)) {
			m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		}
		m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		state, _ = form.FormState()
		require.Equal(t, "x", state.Fields[0].Value, config.ProviderPresets[preset].ID)
		m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		state, _ = form.FormState()
		require.Zero(t, state.Fields[0].Cursor)
	}
}

func TestSettingsArrowsMoveBetweenTheMenuAndTheSection(t *testing.T) {
	m := New(nil)
	p := &settingsPanel{}
	m.dialog = p
	press := func(code rune) { p.key(m, tea.KeyPressMsg{Code: code}) }
	press(tea.KeyDown)
	require.Equal(t, 1, p.section)
	require.False(t, p.inside)
	press(tea.KeyRight)
	require.True(t, p.inside)
	press(tea.KeyLeft)
	require.False(t, p.inside)
	require.Equal(t, 1, p.section)
}

func TestIntegrationsTurnAFoundLanguageServerOnWithEnter(t *testing.T) {
	bin, root := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gopls"), []byte("#!/bin/sh\n"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0600))
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	global := filepath.Join(t.TempDir(), "global.json")
	require.NoError(t, os.WriteFile(global, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(global)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	_, err := config.Load(root, false)
	require.NoError(t, err)
	useWorkspace(t, root)
	m := New(nil)
	m.width, m.height = 120, 40
	m.openSettingsAt(sectionIntegrations)
	p := m.dialog.(*settingsPanel)
	require.Len(t, p.detected, 1)
	require.Contains(t, ansi.Strip(m.View().Content), "gopls")
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "gopls", config.Get().LSP["gopls"].Command)
	require.Empty(t, p.detected, "a server turned on leaves the list")
	saved, err := os.ReadFile(global)
	require.NoError(t, err)
	require.Contains(t, string(saved), "gopls")
}

func TestPreferencesScrollToKeepTheSelectedOneInView(t *testing.T) {
	p := &settingsPanel{section: sectionShortcuts, values: map[string]string{}, origins: map[string]string{}}
	all := p.settingRows()
	require.Greater(t, len(all), 3)
	p.selected = len(all) - 1
	view := ansi.Strip(strings.Join(p.preferenceRows(60, 10), "\n"))
	require.Contains(t, view, all[len(all)-1].Label)
	require.NotContains(t, view, all[0].Label, "only three preferences fit in ten rows")
	require.Contains(t, view, "───", "a rule separates the preferences")
	p.selected = len(all) - 2
	require.Contains(t, ansi.Strip(strings.Join(p.preferenceRows(60, 10), "\n")), all[len(all)-1].Label, "moving up inside the view does not scroll")
}
