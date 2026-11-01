package core

import (
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestFooterContextBarBoundaries(t *testing.T) {
	m := statusCmp{width: 180}
	state := util.FooterState{ContextWindow: 100, ContextKnown: true}
	for _, tc := range []struct {
		used int64
		bar  string
	}{
		{-1, "[-----] 0%"}, {0, "[-----] 0%"}, {1, "[#----] 1%"},
		{40, "[##---] 40%"}, {85, "[#####] 85%"},
		{100, "[#####] 100%"}, {150, "[#####] 100%"},
	} {
		state.ContextTokens = tc.used
		require.Contains(t, ansi.Strip(m.ViewFor(state)), tc.bar)
	}
	state.ContextTokens, state.ContextWindow = math.MaxInt64, math.MaxInt64
	require.Contains(t, m.metrics(state), "100%", "large counters cannot overflow the percentage")
	state.ContextKnown = false
	require.NotContains(t, m.metrics(state), "%", "unknown usage must not become a fake zero")
	state.ContextWindow = 0
	state.Busy, state.SpeedLive, state.TokensPerSecond = true, true, 42
	require.Equal(t, "~42.0 tok/s", m.metrics(state), "stream speed remains visible without advertised context metadata")
}

func TestFooterProgressKeepsTimeAndSpeedWhileResizing(t *testing.T) {
	state := util.FooterState{ContextWindow: 32768, ContextKnown: true, ContextTokens: 8192,
		Busy: true, SpeedLive: true, TokensPerSecond: 70, Activity: "Generating response",
		ElapsedKnown: true, ElapsedSeconds: 65}
	for _, width := range []int{1, 36, 60, 80, 110, 120, 180} {
		m := statusCmp{width: width}
		view := m.ViewFor(state)
		plain := ansi.Strip(view)
		require.Equal(t, 1, lipgloss.Height(view))
		require.Equal(t, width, lipgloss.Width(view))
		if width >= 36 {
			require.Contains(t, plain, "1m05s")
			require.Contains(t, plain, "Esc")
			require.Contains(t, plain, "Gen")
		}
		if width >= 60 {
			require.Contains(t, plain, "~70.0")
			require.Contains(t, plain, "25%")
		}
		if width >= 80 {
			require.Contains(t, plain, "[##---]")
			before, _, _ := strings.Cut(plain, "[##---]")
			require.Equal(t, "context", m.ActionAt(ansi.StringWidth(before), state))
		}
	}
	for _, rate := range []float64{math.NaN(), math.Inf(1), -1, 0} {
		state.TokensPerSecond = rate
		require.Equal(t, "—", footerRate(state))
	}
	require.Equal(t, "1h01m", footerElapsed(3661))
	require.Equal(t, "0s", footerElapsed(-1))
}

func TestFooterPrioritizesHumanQuestionAndWarningsInCompactTerminal(t *testing.T) {
	state := util.FooterState{ContextWindow: 16384, ContextKnown: true, Busy: true,
		WaitingForAnswer: true, Activity: "Waiting for your answer", ElapsedKnown: true, ElapsedSeconds: 5}
	m := statusCmp{width: 60}
	view := ansi.Strip(m.ViewFor(state))
	require.Contains(t, view, "Waiting for your answer")
	require.Contains(t, view, "5s")
	require.Contains(t, view, "Esc")
	require.NotContains(t, view, "%", "waiting for a human takes precedence over usage at compact widths")
	m.width = 80
	m.info = util.InfoMsg{Type: util.InfoTypeWarn, Msg: "See /error for details"}
	require.Contains(t, ansi.Strip(m.ViewFor(state)), "See /error for details")
	require.Equal(t, "error", m.ActionAt(15, state), "warning navigation stays safe and visible")
}

func TestFooterContextUsesSemanticColorsAndText(t *testing.T) {
	profile, dark, name := theme.ColorProfile(), theme.HasDarkBackground(), theme.CurrentThemeName()
	t.Cleanup(func() { theme.SetColorProfile(profile); theme.SetHasDarkBackground(dark); _ = theme.PreviewTheme(name) })
	require.NoError(t, theme.PreviewTheme("opcode"))
	for _, dark := range []bool{false, true} {
		theme.SetHasDarkBackground(dark)
		theme.SetColorProfile(colorprofile.TrueColor)
		colors := theme.CurrentTheme()
		for _, used := range []int64{25, 85, 95} {
			color := colors.Info()
			if used == 85 {
				color = colors.Warning()
			}
			if used == 95 {
				color = colors.Error()
			}
			state := util.FooterState{ContextWindow: 100, ContextKnown: true, ContextTokens: used}
			view := (statusCmp{width: 180}).ViewFor(state)
			actual := footerGlyph(t, view, "[")
			want := footerGlyph(t, styles.BaseStyle().Background(colors.BackgroundSecondary()).Foreground(color).Render("["), "[")
			require.Equal(t, want.FG, actual.FG)
			require.Equal(t, want.BG, actual.BG)
		}
	}
	theme.SetColorProfile(colorprofile.NoTTY)
	view := (statusCmp{width: 180}).ViewFor(util.FooterState{ContextWindow: 100, ContextKnown: true, ContextTokens: 95})
	require.Contains(t, ansi.Strip(view), "[#####] 95%", "color is never the only usage signal")
}
