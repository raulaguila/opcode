package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Box is the proposal's panel: a rounded border whose top edge carries the
// title on the left and a short note on the right, and whose bottom edge
// carries the key legend. Every dialog and card is a Box.
type Box struct {
	Title, Meta, Legend string
	Width               int // outer width, borders included
	PadX, PadY          int
	Accent, Background  color.Color
	TitleColor          color.Color // defaults to Strong
	Dashed              bool        // a dashed edge, for what is off or only offered
	Outer               color.Color // the ground the edge sits on; Background when nil
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
		outer = bg
	}
	// The edge sits on the ground around the box, so a tinted card stays
	// inside its rounded corners.
	edge := on(outer, accent)
	h, v := "─", "│"
	if b.Dashed {
		h, v = "┄", "┆"
	}
	inner := max(1, b.Width-2)
	content := max(1, inner-2*b.PadX)
	label := func(text string, c color.Color, bold bool) string {
		return on(outer, c).Bold(bold).Render(text)
	}
	border := func(left, right, title, meta string, titleStyle func(string) string) string {
		// "╭─ title ──── meta ─╮": labels sit two cells in from the corners.
		meta = ansi.Truncate(meta, max(0, (inner-7)/2), "…")
		if ansi.StringWidth(meta) < 2 {
			meta = ""
		}
		reserved := 0
		if meta != "" {
			reserved = ansi.StringWidth(meta) + 3
		}
		title = ansi.Truncate(title, max(0, inner-4-reserved), "…")
		used := 0
		var out strings.Builder
		out.WriteString(edge.Render(left + h))
		if title != "" {
			out.WriteString(edge.Render(" ") + titleStyle(title) + edge.Render(" "))
			used += ansi.StringWidth(title) + 2
		}
		tail := ""
		if meta != "" {
			tail = edge.Render(" ") + label(meta, P.Muted, false) + edge.Render(" ")
			used += ansi.StringWidth(meta) + 2
		}
		out.WriteString(edge.Render(strings.Repeat(h, max(0, inner-2-used))))
		out.WriteString(tail + edge.Render(h+right))
		return out.String()
	}
	fill := lipgloss.NewStyle().Background(bg)
	pad := fill.Render(strings.Repeat(" ", b.PadX))
	blank := edge.Render(v) + fill.Render(strings.Repeat(" ", inner)) + edge.Render(v)
	rows := []string{border("╭", "╮", b.Title, b.Meta, func(s string) string { return label(s, titleColor, true) })}
	for range b.PadY {
		rows = append(rows, blank)
	}
	for _, line := range strings.Split(body, "\n") {
		line = ansi.Truncate(line, content, "…")
		gap := fill.Render(strings.Repeat(" ", max(0, content-ansi.StringWidth(line))))
		rows = append(rows, edge.Render(v)+pad+line+gap+pad+edge.Render(v))
	}
	for range b.PadY {
		rows = append(rows, blank)
	}
	rows = append(rows, border("╰", "╯", b.Legend, "", func(s string) string { return label(s, P.Muted, false) }))
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
	return []string{on(bg, P.Rule).Render(strings.Repeat("─", width)), Spread(strings.Join(left, gap), right, width, bg)}
}

// Chip is a short tag on a raised tint: "ferramentas", "Ctrl+O".
func Chip(text string, fg, bg color.Color) string {
	return on(P.Raised, fg).Render(" "+text+" ") + on(bg, bg).Render(" ")
}

// ThinBox frames rows with eighth-block lines, the nearest a terminal gets
// to a one-pixel border: the edges hug the fill instead of taking a cell
// of their own. Corners stay open, which reads as slightly rounded. A
// dashed frame breaks its top and bottom lines.
func ThinBox(body []string, width int, edge, fill, outer color.Color, dashed bool) []string {
	inner := max(1, width-2)
	line := func(mark string) string {
		run := strings.Repeat(mark, inner)
		if dashed {
			run = strings.Repeat(mark+" ", inner/2) + strings.Repeat(mark, inner%2)
		}
		return on(outer, edge).Render(" " + run + " ")
	}
	rows := []string{line("▁")}
	for _, row := range body {
		rows = append(rows, on(outer, edge).Render("▕")+Line(row, inner, fill)+on(outer, edge).Render("▏"))
	}
	return append(rows, line("▔"))
}

// OutlineButtons draws buttons with thin outlines over three rows: a line
// above, the labels between side edges, a line below.
func OutlineButtons(labels []string, edge, text, bg color.Color) [3]string {
	var out [3]string
	for i, label := range labels {
		if i > 0 {
			for r := range out {
				out[r] += on(bg, bg).Render(" ")
			}
		}
		span := ansi.StringWidth(label) + 2
		out[0] += on(bg, edge).Render(" " + strings.Repeat("▁", span) + " ")
		out[1] += on(bg, edge).Render("▕") + on(bg, text).Render(" "+label+" ") + on(bg, edge).Render("▏")
		out[2] += on(bg, edge).Render(" " + strings.Repeat("▔", span) + " ")
	}
	return out
}
