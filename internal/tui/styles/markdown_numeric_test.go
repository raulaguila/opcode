package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestMarkdownKeepsDecimalPercentTokensTogetherWhenTheyFit(t *testing.T) {
	for _, token := range []string{"+173,5%", "+185,4%", "+247,1%", "−32,5%", "173.5%"} {
		for words := range 20 {
			source := "- " + strings.Repeat("custo ", words) + token + " de objetos no Markdown."
			view := ansi.Strip(RenderMarkdownBlocks(source, 40, 40))
			require.Contains(t, view, token, "wrapping must not separate decimal components: %q", source)
		}
	}
}
