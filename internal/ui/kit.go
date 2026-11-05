package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Box is the proposal's panel: a framed fill whose top edge carries the
// title on the left and a short note on the right, and whose bottom edge
// can carry a legend. Every dialog and card is a Box.
//
// The frame is drawn with eighth-block lines (▁ ▔ ▕ ▏) that sit on the cell
// edge next to the fill, the nearest a terminal gets to a thin border: a
// box-drawing line (│) sits mid-cell and leaves half a cell of the wrong
// color on one side. The corner cells stay empty, which reads as rounded.
type Box struct {
	Title, Meta, Legend string
	Width               int // outer width, edges included
	PadX, PadY          int
	Accent, Background  color.Color
	TitleColor          color.Color // defaults to Strong
	Outer               color.Color // the ground the edge sits on; the screen's Ground when nil
}

// Render draws the box around body; body lines are clipped and padded.
func (b Box) Render(body string) string {
	bg := b.Background
	if bg == nil {
		bg = P.Surface
	}
	accent := b.Accent
	if accent == nil {
		accent = P.Border
	}
	titleColor := b.TitleColor
	if titleColor == nil {
		titleColor = P.Strong
	}
	outer := b.Outer
	if outer == nil {
		outer = P.Ground
	}
	edge := on(outer, accent)
	inner := max(1, b.Width-2)
	content := max(1, inner-2*b.PadX)
	label := func(text string, c color.Color, bold bool) string {
		return on(outer, c).Bold(bold).Render(text)
	}
	// line draws a top or bottom edge, labels sitting on it two cells in.
	line := func(mark, title, meta string, titleStyle func(string) string) string {
		run := func(n int) string { return strings.Repeat(mark, max(0, n)) }
		meta = ansi.Truncate(meta, max(0, (inner-7)/2), "…")
		if ansi.StringWidth(meta) < 2 {
			meta = ""
		}
		reserved := 0
		if meta != "" {
			reserved = ansi.StringWidth(meta) + 3
		}
		title = ansi.Truncate(title, max(0, inner-4-reserved), "…")
		used := 1
		var out strings.Builder
		out.WriteString(edge.Render(" " + run(1)))
		if title != "" {
			out.WriteString(edge.Render(" ") + titleStyle(title) + edge.Render(" "))
			used += ansi.StringWidth(title) + 2
		}
		tail, tailWidth := "", 0
		if meta != "" {
			tail = edge.Render(" ") + label(meta, P.Muted, false) + edge.Render(" ")
			tailWidth = ansi.StringWidth(meta) + 2
			used += tailWidth
		}
		out.WriteString(edge.Render(run(inner - 1 - used)))
		out.WriteString(tail + edge.Render(run(1)+" "))
		return out.String()
	}
	left, right := edge.Render("▕"), edge.Render("▏")
	fill := lipgloss.NewStyle().Background(bg)
	pad := fill.Render(strings.Repeat(" ", b.PadX))
	blank := left + fill.Render(strings.Repeat(" ", inner)) + right
	rows := []string{line("▁", b.Title, b.Meta, func(s string) string { return label(s, titleColor, true) })}
	for range b.PadY {
		rows = append(rows, blank)
	}
	for _, row := range strings.Split(body, "\n") {
		row = ansi.Truncate(row, content, "…")
		gap := fill.Render(strings.Repeat(" ", max(0, content-ansi.StringWidth(row))))
		rows = append(rows, left+pad+row+gap+pad+right)
	}
	for range b.PadY {
		rows = append(rows, blank)
	}
	rows = append(rows, line("▔", b.Legend, "", func(s string) string { return label(s, P.Muted, false) }))
	return strings.Join(rows, "\n")
}

// Inner is the writable width inside a box with the given outer width.
func (b Box) Inner() int { return max(1, b.Width-2-2*b.PadX) }

// Button is a filled label. The primary one takes the accent; the others sit
// on a raised surface.
func Button(label string, primary bool, accent color.Color) string {
	if primary {
		return on(accent, P.Field).Bold(true).Render(" " + label + " ")
	}
	return on(P.Raised, P.Strong).Render(" " + label + " ")
}

// Line pads text to width on a background, so rows paint edge to edge.
func Line(text string, width int, bg color.Color) string {
	text = ansi.Truncate(text, width, "…")
	return text + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", max(0, width-ansi.StringWidth(text))))
}

// Spread places left and right text on one row of the given width.
func Spread(left, right string, width int, bg color.Color) string {
	room := max(0, width-ansi.StringWidth(right)-1) // keep a gap between the two
	left = ansi.Truncate(left, room, "…")
	gap := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right))))
	return left + gap + right
}

// Meter draws a five-cell usage bar: "██░░░".
func Meter(fraction float64, color color.Color, bg color.Color) string {
	filled := min(5, max(0, int(fraction*5)))
	return on(bg, color).Render(strings.Repeat("█", filled)) + on(bg, P.Faint).Render(strings.Repeat("░", 5-filled))
}

// KeyRow is a panel's footer: "Enter use · Tab provider · Esc close" with the
// keys bright and the last pair pushed right, under a rule.
func KeyRow(legend string, width int, bg color.Color) []string {
	pair := func(segment string) string {
		key, desc, _ := strings.Cut(segment, " ")
		return on(bg, P.Strong).Bold(true).Render(key) + on(bg, P.Muted).Render(" "+desc)
	}
	parts := strings.Split(legend, " · ")
	gap := on(bg, bg).Render("   ")
	var left []string
	for _, part := range parts[:len(parts)-1] {
		left = append(left, pair(part))
	}
	right := pair(parts[len(parts)-1])
	if len(parts) == 1 {
		return []string{on(bg, P.Rule).Render(strings.Repeat("─", width)), Line(right, width, bg)}
	}
	rows := []string{on(bg, P.Rule).Render(strings.Repeat("─", width))}
	line := left[0]
	for _, part := range left[1:] { // a legend that does not fit keeps every key, on more rows
		if ansi.StringWidth(line+gap+part)+ansi.StringWidth(right)+1 > width {
			rows = append(rows, Line(line, width, bg))
			line = part
			continue
		}
		line += gap + part
	}
	return append(rows, Spread(line, right, width, bg))
}

// Chip is a short tag on a raised tint: "ferramentas", "Ctrl+O".
func Chip(text string, fg, bg color.Color) string {
	return on(P.Raised, fg).Render(" "+text+" ") + on(bg, bg).Render(" ")
}
