package tui

import (
	"image/color"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

func placeModal(x, y int, foreground, background string) string {
	return layout.PlaceOverlay(x, y, foreground, attenuateModalBackground(background), false)
}

// Neutralize background presentation only. The original page, selections and
// modal geometry remain intact. No color mode uses faint text and preserves
// inverse selection without introducing colors.
func attenuateModalBackground(content string) string {
	if theme.ColorProfile() <= colorprofile.NoTTY {
		return content
	}
	t := theme.CurrentTheme()
	style := styles.BaseStyle().Foreground(t.TextMuted()).Background(t.Background())
	if theme.ColorProfile() == colorprofile.Ascii {
		style = style.Faint(true)
	}
	return style.Render(modalBackgroundText(content))
}

func modalBackgroundText(content string) string {
	var out strings.Builder
	out.Grow(len(content))
	parser := new(ansi.Parser)
	parser.SetParamsSize(32)
	parser.SetDataSize(1)
	parser.SetHandler(ansi.Handler{
		Print: func(r rune) { out.WriteRune(r) },
		Execute: func(b byte) {
			if b == '\n' || b == '\t' {
				out.WriteByte(b)
			}
		},
		HandleCsi: func(cmd ansi.Cmd, params ansi.Params) {
			if cmd.Final() == 'm' {
				out.WriteString(modalSelectionAttributes(params))
			}
		},
	})
	parser.Parse([]byte(content))
	return out.String()
}

// Selection remains recognizable while colors, bold and headings are muted.
func modalSelectionAttributes(params ansi.Params) string {
	var out strings.Builder
	for i := 0; i < len(params); i++ {
		switch params[i].Param(0) {
		case 38, 48, 58:
			var ignored color.Color
			i += max(1, ansi.ReadStyleColor(params[i:], &ignored)) - 1
		case 0, 27:
			out.WriteString("\x1b[27m")
		case 7:
			out.WriteString("\x1b[7m")
		}
	}
	return out.String()
}
