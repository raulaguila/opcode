package core

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func compactTokens(n int64) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
	if n >= 1024 {
		if n%1024 == 0 {
			return fmt.Sprintf("%dk", n/1024)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1024)
	}
	return fmt.Sprint(n)
}

func contextPercent(state util.FooterState) float64 {
	if state.ContextWindow <= 0 {
		return 0
	}
	return min(100, max(0, float64(state.ContextTokens)/float64(state.ContextWindow)*100))
}

func (m statusCmp) contextMetric(state util.FooterState) string {
	if state.ContextWindow <= 0 {
		return ""
	}
	if !state.ContextKnown {
		return "—/" + compactTokens(state.ContextWindow) + " ctx"
	}
	percent := contextPercent(state)
	label := fmt.Sprintf("%.0f%%", math.Floor(percent))
	prefix := ""
	if state.ContextEstimated {
		prefix = "~"
	}
	if m.width < 80 {
		return prefix + label + " ctx"
	}
	// ASCII cells work with NO_COLOR and terminals without block glyphs.
	filled := min(5, int(math.Floor(percent/20)))
	bar := "[" + strings.Repeat("#", filled) + strings.Repeat("-", 5-filled) + "] " + prefix + label
	if m.width < 110 {
		return bar
	}
	return prefix + compactTokens(max(0, state.ContextTokens)) + "/" + compactTokens(state.ContextWindow) + " ctx " + bar
}

func footerRate(state util.FooterState) string {
	rate := state.TokensPerSecond
	if state.AwaitingModel || state.Busy && !state.SpeedLive || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return "—"
	}
	label := fmt.Sprintf("%.1f", rate)
	if rate > 9999 {
		label = fmt.Sprintf("%.3g", rate)
	}
	if state.SpeedEstimated || state.SpeedLive {
		label = "~" + label
	}
	return label
}

func (m statusCmp) metrics(state util.FooterState) string {
	if m.width < 60 || m.width < 80 && state.WaitingForAnswer || m.width < 110 && m.hasPriorityInfo() {
		return ""
	}
	context := m.contextMetric(state)
	// Speed does not depend on whether the provider advertises a window.
	if context == "" && !state.Busy && state.TokensPerSecond <= 0 {
		return ""
	}
	unit := " tok/s"
	if m.width < 110 {
		unit = " t/s"
	}
	if context != "" {
		context += " · "
	}
	return context + footerRate(state) + unit
}

func (m statusCmp) metricsSegment(state util.FooterState, width int) string {
	if width == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	style := styles.BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.Text())
	context := m.contextMetric(state)
	metric := m.metrics(state)
	if state.ContextKnown && context != "" {
		color := t.Info()
		if contextPercent(state) >= 85 {
			color = t.Warning()
		}
		if contextPercent(state) >= 95 {
			color = t.Error()
		}
		metric = style.Foreground(color).Render(context) + strings.TrimPrefix(metric, context)
	}
	return style.Width(width).MaxWidth(width).Align(lipgloss.Right).Render(ansi.Truncate(" "+metric, width, "…"))
}

func footerElapsed(seconds int) string {
	seconds = max(0, seconds)
	if seconds >= 3600 {
		return fmt.Sprintf("%dh%02dm", seconds/3600, seconds/60%60)
	}
	if seconds >= 60 {
		return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (m statusCmp) hasPriorityInfo() bool {
	return m.info.Msg != "" && (m.info.Type == util.InfoTypeWarn || m.info.Type == util.InfoTypeError)
}

// Keep elapsed time and cancellation visible when a phase label needs clipping.
func compactFooterActivity(text string, width int, state util.FooterState) string {
	if ansi.StringWidth(text) <= width {
		return text
	}
	suffix := " Esc"
	if state.ElapsedKnown {
		suffix = " " + footerElapsed(state.ElapsedSeconds) + suffix
	}
	phase := footerActivityLabel(state)
	phase, _, _ = strings.Cut(phase, " ")
	phase = strings.TrimSuffix(phase, "…")
	if phase == "Investigating" {
		phase = config.Text("Investigando", "Exploring")
	}
	if state.Pending > 0 {
		phase = fmt.Sprintf("%d + ", state.Pending) + phase
	}
	return ansi.Truncate(phase, max(0, width-ansi.StringWidth(suffix)), "…") + suffix
}
