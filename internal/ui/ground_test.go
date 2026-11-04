package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/ui/styles"
	"github.com/raulaguila/opcode/internal/ui/theme"
	"github.com/stretchr/testify/require"
)

// The bar ends with a reset, so a row that does not set its own color shows
// the terminal's background between the bar and the first change of color.
func TestEveryReplyRowStartsOnTheApplicationGround(t *testing.T) {
	profile, dark := theme.ColorProfile(), theme.HasDarkBackground()
	t.Cleanup(func() { theme.SetColorProfile(profile); theme.SetHasDarkBackground(dark) })
	theme.SetColorProfile(colorprofile.TrueColor)
	theme.SetHasDarkBackground(true)
	require.NoError(t, theme.PreviewTheme("tokyonight"))
	theme.UseGround()

	source := "Texto longo que precisa quebrar a linha porque passa da largura **forte** e `code` e continua ate o fim\n\n1. **Propósito:** um agente de desenvolvimento que quebra a linha aqui\n2. `cmd/app.go` inicia\n\n- **item** algo\n\n> citacao **forte**\n\n| a | b |\n|---|---|\n| 1 | `x` |\n"
	body := styles.RenderMarkdownBlocks(source, 50, 50)
	ground := regexp.MustCompile(`^(\x1b\[[0-9;]*m)*\x1b\[48;2;22;22;30m`)
	for line := range strings.SplitSeq(body, "\n") {
		if ansi.Strip(line) != "" {
			require.Regexp(t, ground, line, ansi.Strip(line))
		}
	}
}
