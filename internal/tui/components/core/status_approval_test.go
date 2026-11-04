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

func TestFooterAlwaysShowsApprovalControlWithoutMode(t *testing.T) {
	for _, mode := range []string{"ask", "plan", "execute"} {
		for _, policy := range []string{"prompt", "sandboxed"} {
			for _, width := range []int{36, 60, 80, 110, 120, 180} {
				assertApprovalControls(t, width, util.FooterState{Mode: mode, ApprovalPolicy: policy})
			}
		}
	}
}

func TestPortugueseFooterKeepsApprovalTimeContextAndSpeed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := config.Get()
	if cfg == nil {
		var err error
		cfg, err = config.Load(t.TempDir(), false)
		require.NoError(t, err)
	}
	previous := cfg.TUI.Language
	t.Cleanup(func() { cfg.TUI.Language = previous })
	cfg.TUI.Language = "pt"
	for _, mode := range []string{"ask", "plan", "execute"} {
		for _, policy := range []string{"prompt", "sandboxed"} {
			for _, width := range []int{36, 60, 80, 110, 120, 180} {
				state := util.FooterState{Mode: mode, ApprovalPolicy: policy, Activity: "Gerando resposta", Busy: true, ElapsedKnown: true, ElapsedSeconds: 65, ContextKnown: true, ContextWindow: 32768, ContextTokens: 8192, SpeedLive: true, TokensPerSecond: 70}
				assertApprovalControls(t, width, state)
				view := ansi.Strip((statusCmp{width: width}).ViewFor(state))
				require.Contains(t, view, "Gerando")
				require.Contains(t, view, "1m05s")
				require.Contains(t, view, "Esc")
				if width >= 60 {
					require.Contains(t, view, "~70.0")
					require.Contains(t, view, "25%")
				}
			}
		}
	}
}

func assertApprovalControls(t *testing.T, width int, state util.FooterState) {
	t.Helper()
	m := statusCmp{width: width}
	view := ansi.Strip(m.ViewFor(state))
	require.Equal(t, 1, lipgloss.Height(view))
	require.Equal(t, width, lipgloss.Width(view))
	label := config.Text("confirmar", "confirm")
	if state.ApprovalPolicy == "sandboxed" {
		label = config.Text("pedir", "ask")
	}
	if width >= 160 {
		label = config.Text("Aprovação: confirmar tudo", "Approval: confirm all")
		if state.ApprovalPolicy == "sandboxed" {
			label = config.Text("Aprovação: pedir aprovação", "Approval: ask for approval")
		}
	} else if width >= 110 {
		label = config.Text("Aprov.: ", "Appr.: ") + label
	} else {
		label = "A:" + label
	}
	before, _, found := strings.Cut(view, label)
	require.True(t, found, "%s/%s at %d: %s", state.Mode, state.ApprovalPolicy, width, view)
	start := ansi.StringWidth(before)
	for x := start; x < start+ansi.StringWidth(label); x++ {
		require.Equal(t, "approval", m.ActionAt(x, state))
	}
	require.Equal(t, "approval", m.ActionAt(1, state), "the footer opens with approval; the mode is in the composer")
	require.NotContains(t, view, "Perguntar ▾")
	if width >= 60 {
		require.Equal(t, 2, strings.Count(view, "▾"))
		require.Equal(t, "models", m.ActionAt(width-2, state))
	}
	require.Empty(t, m.ActionAt(width-1, state))
}
