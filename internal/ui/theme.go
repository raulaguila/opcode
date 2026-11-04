// Package ui is the terminal interface drawn from the approved visual
// proposal. It renders on top of the existing app services; it owns no
// agent, permission or storage logic.
package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/ui/theme"
)

// Palette holds the proposal's colors. One dark world: the terminal is used
// at a desk, often beside an editor, and the proposal is drawn dark.
type Palette struct {
	Ground, Surface, Raised, Field     color.Color
	Rule, Border, BorderStrong         color.Color
	Text, Strong, Muted, Faint, Dim    color.Color
	Ask, Plan, Execute, User, Info     color.Color
	Success, Danger, Selected, Warning color.Color
	// Card grounds, each tinted toward its accent, and the diff rows.
	AskCard, PlanCard, ApprovalCard, DangerCard, DangerEdge, PlanStep color.Color
	Added, AddedText, AddedNumber, Removed, RemovedText, Segment      color.Color
	Card, CardEdge, CardSelected, CardSelectedEdge                    color.Color
}

var P = Palette{
	Ground:           theme.Ground,
	Surface:          lipgloss.Color("#1b1c29"),
	Raised:           lipgloss.Color("#1f2030"),
	Field:            lipgloss.Color("#13131b"),
	Rule:             lipgloss.Color("#23253a"),
	Border:           lipgloss.Color("#3b3e57"),
	BorderStrong:     lipgloss.Color("#4a4e6d"),
	Text:             lipgloss.Color("#c8cbe0"),
	Strong:           lipgloss.Color("#e1e3f0"),
	Muted:            lipgloss.Color("#8a8fa8"),
	Faint:            lipgloss.Color("#6f7490"),
	Dim:              lipgloss.Color("#4d5069"),
	Ask:              lipgloss.Color("#7aa2f7"),
	Plan:             lipgloss.Color("#e0af68"),
	Execute:          lipgloss.Color("#ff9e64"),
	User:             lipgloss.Color("#bb9af7"),
	Info:             lipgloss.Color("#7dcfff"),
	Success:          lipgloss.Color("#9ece6a"),
	Danger:           lipgloss.Color("#f7768e"),
	Selected:         lipgloss.Color("#262840"),
	Warning:          lipgloss.Color("#e0af68"),
	AskCard:          lipgloss.Color("#181a28"),
	PlanCard:         lipgloss.Color("#1a1a22"),
	ApprovalCard:     lipgloss.Color("#1a1924"),
	DangerCard:       lipgloss.Color("#1d1a26"),
	DangerEdge:       lipgloss.Color("#4a2b38"),
	PlanStep:         lipgloss.Color("#262420"),
	Added:            lipgloss.Color("#18261c"),
	AddedText:        lipgloss.Color("#b5e08a"),
	AddedNumber:      lipgloss.Color("#6f9a5a"),
	Removed:          lipgloss.Color("#2a1a22"),
	RemovedText:      lipgloss.Color("#f0a0b0"),
	Segment:          lipgloss.Color("#2f3352"),
	Card:             lipgloss.Color("#1b1c29"),
	CardEdge:         lipgloss.Color("#2f3147"),
	CardSelected:     lipgloss.Color("#1f2236"),
	CardSelectedEdge: lipgloss.Color("#3d4a6e"),
}

// on returns a style with the given foreground on the given background.
func on(bg, fg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Background(bg).Foreground(fg)
}

// ModeColor is the accent of a work mode: Ask blue, Plan amber, Execute orange.
func ModeColor(mode string) color.Color {
	switch mode {
	case "ask":
		return P.Ask
	case "plan":
		return P.Plan
	}
	return P.Execute
}
