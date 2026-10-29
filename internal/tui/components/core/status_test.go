package core

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestFooterStaysOnOneRowAndMouseMatchesVisibleSegments(t *testing.T) {
	for _, width := range []int{1, 12, 36, 80, 160} {
		m := statusCmp{width: width}
		state := util.FooterState{Mode: "plan"}
		view := m.ViewFor(state)
		require.Equal(t, 1, lipgloss.Height(view))
		require.Equal(t, width, lipgloss.Width(view))
		require.True(t, strings.HasSuffix(ansi.Strip(view), " "), "last cell protects the control from erase-after-wrap")
		require.Empty(t, m.ActionAt(width-1, state), "the terminal margin is not a model control")
		if width >= 36 {
			plain := ansi.Strip(view)
			modeEnd := strings.Index(plain, "▾")
			require.Positive(t, modeEnd)
			require.Equal(t, "mode", m.ActionAt(1, state))
			require.Equal(t, "commands", m.ActionAt(width/2, state))
			require.Equal(t, "models", m.ActionAt(width-2, state))
			require.NotContains(t, plain, "Context:")
			require.NotContains(t, plain, "Cost:")
			require.NotContains(t, plain, "No diagnostics")
		}
		require.Empty(t, m.ActionAt(width, state))
	}
}

func TestFooterKeepsWorkQueueAndErrorsVisibleInsteadOfRoutineToasts(t *testing.T) {
	m := statusCmp{width: 100, info: util.InfoMsg{Msg: "routine toast"}}
	state := util.FooterState{Mode: "ask", Activity: "Lendo arquivos", Busy: true, Pending: 2}
	view := ansi.Strip(m.ViewFor(state))
	require.Contains(t, view, "Lendo arquivos")
	require.Contains(t, view, "2 queued")
	require.Contains(t, view, "Esc cancels")
	require.NotContains(t, view, "routine toast")
	require.Equal(t, "queue", m.ActionAt(25, state))
	m.info = util.InfoMsg{Type: util.InfoTypeError, Msg: "Autenticação necessária"}
	require.Contains(t, ansi.Strip(m.ViewFor(state)), "Autenticação necessária")
	state.Busy, state.Pending = false, 0
	state.Activity = "Partial work"
	m.info = util.InfoMsg{Msg: "routine toast"}
	require.Contains(t, ansi.Strip(m.ViewFor(state)), "Partial work")
}

func TestFooterAdvertisesOnlyConfirmedEnhancedNewline(t *testing.T) {
	m := statusCmp{width: 180}
	require.Contains(t, ansi.Strip(m.View()), "Alt+Enter")
	updated, _ := m.Update(util.EnhancedKeyboardMsg{Supported: true})
	require.Contains(t, ansi.Strip(updated.View()), "Shift+Enter")
}

func TestWarningClickFollowsVisibleSafeResolution(t *testing.T) {
	m := statusCmp{width: 100, info: util.InfoMsg{Type: util.InfoTypeWarn, Msg: "LSP unavailable; see /integrations"}}
	state := util.FooterState{Busy: true, Pending: 2}
	require.Equal(t, "integrations", m.ActionAt(25, state))
	require.Contains(t, ansi.Strip(m.ViewFor(state)), "Warning:")
	m.info.Msg = "See /retry" // Clicking a hint must never resume tool execution.
	require.Equal(t, "queue", m.ActionAt(25, state))
}
