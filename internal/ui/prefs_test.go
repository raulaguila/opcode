package ui

import (
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingOptionsReadChoicesButNotRanges(t *testing.T) {
	require.Equal(t, []string{"pt", "en"}, settingOptions(config.Setting{Help: "pt, en"}))
	require.Equal(t, []string{"off", "bell", "auto", "native", "osc"}, settingOptions(config.Setting{Help: "off, bell, auto, native, osc (only when unfocused)"}))
	require.Nil(t, settingOptions(config.Setting{Help: "1–200"}))
	require.Nil(t, settingOptions(config.Setting{Help: "ctrl+letter, alt+letter"}))
	require.Equal(t, "Language", localLabel("Idioma / Language"))
}
