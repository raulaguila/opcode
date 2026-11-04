package core

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestCompactIdleFooterKeepsRecognizableModelAndUsage(t *testing.T) {
	for _, width := range []int{60, 70, 79} {
		for _, activity := range []string{"", config.Text("Concluído", "Completed")} {
			state := util.FooterState{Mode: "execute", ApprovalPolicy: "sandboxed", Activity: activity, ContextWindow: 32768, ContextTokens: 8192, ContextKnown: true, TokensPerSecond: 70}
			m := statusCmp{width: width}
			plain := ansi.Strip(m.ViewFor(state))
			require.Equal(t, 1, lipgloss.Height(plain))
			require.Equal(t, width, lipgloss.Width(plain))
			_, _, label := m.labels(state)
			_, _, _, modelWidth := m.metricWidths(state)
			model := ansi.Cut(plain, width-1-modelWidth, width-1)
			require.Contains(t, model, ansi.Truncate(strings.TrimSuffix(label, " ▾"), 3, ""), "model identity must survive compact idle layout")
			require.Contains(t, plain, "A:"+config.Text("pedir", "ask"))
			require.Contains(t, plain, "25%")
			require.Contains(t, plain, "70.0 t/s")
			require.Equal(t, "models", m.ActionAt(width-2, state))
		}
	}
}

func TestCompactFooterReservesSpaceForActiveOrImportantStates(t *testing.T) {
	m := statusCmp{width: 60}
	for _, state := range []util.FooterState{
		{Busy: true}, {Terminal: true}, {Pending: 1}, {Activity: "Run interrupted"},
	} {
		require.False(t, m.compactIdle(state))
	}
	m.info = util.InfoMsg{Type: util.InfoTypeWarn, Msg: "See /error"}
	require.False(t, m.compactIdle(util.FooterState{}))
	require.Contains(t, ansi.Strip(m.View()), "See /error")
}
