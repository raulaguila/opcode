// Package ui is the terminal interface drawn from the approved visual
// proposal. It renders on top of the existing app services; it owns no
// agent, permission or storage logic.
package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Palette holds the proposal's colors. One dark world: the terminal is used
// at a desk, often beside an editor, and the proposal is drawn dark.
type Palette struct {
	Ground, Surface, Raised, Field     color.Color
	Rule, Border, BorderStrong         color.Color
	Text, Strong, Muted, Faint         color.Color
	Ask, Plan, Execute, User, Info     color.Color
	Success, Danger, Selected, Warning color.Color
}

var P = Palette{
	Ground:       lipgloss.Color("#16161e"),
	Surface:      lipgloss.Color("#1b1c29"),
	Raised:       lipgloss.Color("#1f2030"),
	Field:        lipgloss.Color("#13131b"),
	Rule:         lipgloss.Color("#23253a"),
	Border:       lipgloss.Color("#3b3e57"),
	BorderStrong: lipgloss.Color("#4a4e6d"),
	Text:         lipgloss.Color("#c8cbe0"),
	Strong:       lipgloss.Color("#e1e3f0"),
	Muted:        lipgloss.Color("#8a8fa8"),
	Faint:        lipgloss.Color("#6f7490"),
	Ask:          lipgloss.Color("#7aa2f7"),
	Plan:         lipgloss.Color("#e0af68"),
	Execute:      lipgloss.Color("#ff9e64"),
	User:         lipgloss.Color("#bb9af7"),
	Info:         lipgloss.Color("#7dcfff"),
	Success:      lipgloss.Color("#9ece6a"),
	Danger:       lipgloss.Color("#f7768e"),
	Selected:     lipgloss.Color("#262840"),
	Warning:      lipgloss.Color("#e0af68"),
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
