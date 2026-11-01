package styles

import (
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

func Panel() lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).
		Border(lipgloss.RoundedBorder()).BorderForeground(t.TextMuted()).
		BorderBackground(t.BackgroundSecondary())
}

func PanelTitle() lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.Primary()).Bold(true)
}

// PanelHint is for application navigation legends, never response prose or
// permission decisions. Some themes use comment colors too faint for controls.
func PanelHint() lipgloss.Style {
	return HintOn(theme.CurrentTheme().BackgroundSecondary())
}

func HintOn(background color.Color) lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(background).Foreground(ReadableForeground(t.TextMuted(), background)).
		Bold(false).Faint(false).Italic(false).Underline(false).Reverse(false)
}

// ReadableForeground preserves the preferred semantic color when it is readable.
func ReadableForeground(preferred, background color.Color) color.Color {
	foreground := preferred
	if panelContrast(foreground, background) < 4.5 {
		foreground = theme.CurrentTheme().Text()
	}
	if panelContrast(foreground, background) < 4.5 {
		foreground = color.White
		if panelContrast(color.Black, background) > panelContrast(foreground, background) {
			foreground = color.Black
		}
	}
	return foreground
}

func panelContrast(a, b color.Color) float64 {
	x, y := panelLuminance(a), panelLuminance(b)
	return (max(x, y) + 0.05) / (min(x, y) + 0.05)
}

func panelLuminance(value color.Color) float64 {
	r, g, b, _ := value.RGBA()
	var result float64
	for i, component := range []uint32{r, g, b} {
		v := float64(component) / 65535
		if v <= 0.04045 {
			v /= 12.92
		} else {
			v = math.Pow((v+0.055)/1.055, 2.4)
		}
		result += v * []float64{0.2126, 0.7152, 0.0722}[i]
	}
	return result
}

// SelectedRow tints the selected line with the accent, kept readable on its
// background; the "› " mark still identifies it without color.
func SelectedRow() lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BorderDim()).Foreground(ReadableForeground(t.Primary(), t.BorderDim()))
}

func PanelDivider(width int) string {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.TextMuted()).
		Render(strings.Repeat("─", max(0, width)))
}

// Frame draws a rounded panel whose top border carries the title (and a
// right-aligned meta) and whose bottom border carries the key legend, so a
// dialog spends no rows on headings or dividers. Body lines are padded to
// innerWidth; padX/padY are the blank cells between border and body.
func Frame(body string, innerWidth, padX, padY int, title, meta, hint string) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	border := BaseStyle().Background(bg).Foreground(t.TextMuted())
	span := innerWidth + 2*padX
	edge := func(left, right string, label lipgloss.Style, text, trailing string) string {
		// "╭─ title ───── meta ─╮": labels sit two cells in from each corner.
		// The meta (a position or count) is short and wins room over the title.
		trailing = ansi.Truncate(trailing, max(0, (span-7)/2), "…")
		if ansi.StringWidth(trailing) < 2 {
			trailing = ""
		}
		reserved := 0
		if trailing != "" {
			reserved = ansi.StringWidth(trailing) + 3
		}
		text = ansi.Truncate(text, max(0, span-4-reserved), "…")
		used := 0
		var b strings.Builder
		b.WriteString(border.Render(left + "─"))
		if text != "" {
			b.WriteString(border.Render(" ") + label.Render(text) + border.Render(" "))
			used += ansi.StringWidth(text) + 2
		}
		tail := ""
		if trailing != "" {
			tail = border.Render(" ") + PanelHint().Render(trailing) + border.Render(" ")
			used += ansi.StringWidth(trailing) + 2
		}
		b.WriteString(border.Render(strings.Repeat("─", max(0, span-1-used-1))))
		b.WriteString(tail + border.Render("─"+right))
		return b.String()
	}
	fill := BaseStyle().Background(bg)
	pad := fill.Render(strings.Repeat(" ", padX))
	blank := border.Render("│") + fill.Render(strings.Repeat(" ", span)) + border.Render("│")
	rows := []string{edge("╭", "╮", PanelTitle(), title, meta)}
	for range padY {
		rows = append(rows, blank)
	}
	for line := range strings.SplitSeq(body, "\n") {
		line = ansi.Truncate(line, innerWidth, "…")
		gap := fill.Render(strings.Repeat(" ", max(0, innerWidth-ansi.StringWidth(line))))
		rows = append(rows, border.Render("│")+pad+line+gap+pad+border.Render("│"))
	}
	for range padY {
		rows = append(rows, blank)
	}
	rows = append(rows, edge("╰", "╯", PanelHint(), hint, ""))
	return strings.Join(rows, "\n")
}
