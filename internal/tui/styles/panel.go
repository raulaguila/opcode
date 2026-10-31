package styles

import (
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

func Panel() lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).
		Border(lipgloss.NormalBorder()).BorderForeground(t.TextMuted()).
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

func SelectedRow() lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BorderDim()).Foreground(t.Text())
}

func PanelDivider(width int) string {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.TextMuted()).
		Render(strings.Repeat("─", max(0, width)))
}
