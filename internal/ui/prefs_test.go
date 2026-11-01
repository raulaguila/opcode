package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
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
