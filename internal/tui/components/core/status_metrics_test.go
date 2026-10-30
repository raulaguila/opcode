package core

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestFooterMetricsResizeAndClicksMatchVisibleCells(t *testing.T) {
	state := util.FooterState{ContextKnown: true, ContextTokens: 8192, ContextWindow: 32768, ContextEstimated: true, TokensPerSecond: 24.5}
	for _, width := range []int{36, 60, 80, 120, 180} {
		m := statusCmp{width: width}
		view := m.ViewFor(state)
		plain := ansi.Strip(view)
		require.Equal(t, 1, lipgloss.Height(view))
		require.Equal(t, width, lipgloss.Width(view))
		if width < 72 {
			require.NotContains(t, plain, "ctx")
			continue
		}
		require.Contains(t, plain, "~8k/32k ctx")
		before, _, _ := strings.Cut(plain, "~8k")
		x := ansi.StringWidth(before)
		require.Equal(t, "context", m.ActionAt(x, state))
		require.Equal(t, "models", m.ActionAt(width-2, state))
		if width >= 110 {
			require.Contains(t, plain, "24.5 tok/s")
		} else {
			require.NotContains(t, plain, "tok/s")
		}
	}
	m := statusCmp{width: 180}
	state.Busy = true
	require.Contains(t, ansi.Strip(m.ViewFor(state)), "— tok/s")
	state.Busy = false
	state.SpeedEstimated = true
	require.Contains(t, ansi.Strip(m.ViewFor(state)), "~24.5 tok/s")
	state.Busy, state.SpeedLive = true, true
	require.Contains(t, ansi.Strip(m.ViewFor(state)), "~24.5 tok/s")
	state.AwaitingModel = true
	require.NotContains(t, ansi.Strip(m.ViewFor(state)), "24.5 tok/s")
	require.Contains(t, ansi.Strip(m.ViewFor(state)), config.Text("Aguardando modelo…", "Awaiting model…"))
	state.AwaitingModel, state.TokensPerSecond = false, 0
	require.Contains(t, ansi.Strip(m.ViewFor(state)), config.Text("Gerando…", "Generating…"))
	for _, width := range []int{60, 80, 110, 120, 180} {
		m.width = width
		view := m.ViewFor(state)
		require.Equal(t, 1, lipgloss.Height(view))
		require.Equal(t, width, lipgloss.Width(view))
	}
}
