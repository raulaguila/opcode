package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
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
			{Label: "Nome", Value: "Ollama", Focused: true},
			{Label: "Chave", Value: "sk-secret", Secret: true},
			{Label: "Auth", Value: "none", Options: []string{"none", "api_key"}, OptionLabels: []string{"Nenhuma", "Chave"}},
		},
	}, 100))
	require.Contains(t, view, "Nome             Ollama▏")
	require.NotContains(t, view, "sk-secret")
	require.Contains(t, view, "Auth             ◉ Nenhuma   ○ Chave")
	require.Contains(t, view, "✓ 8 modelos")
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
