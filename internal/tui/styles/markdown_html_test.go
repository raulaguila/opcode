package styles

import (
	"strings"
	"testing"

	terminalansi "github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestMarkdownKeepsHTMLLookingTextVisible(t *testing.T) {
	for _, source := range []string{
		"Planeje o comando 'remove <id>'. Não altere arquivos.",
		"Use Vec<T> e Option<String>.",
		"<arquivo> é obrigatório\nna linha seguinte",
		"Texto com `code <b>` e <br> solto.",
	} {
		plain := terminalansi.Strip(RenderMarkdownBlocks(source, 80, 80))
		for _, want := range []string{"<id>", "Vec<T>", "<arquivo>", "<br>", "code <b>"} {
			if strings.Contains(source, want) {
				require.Contains(t, plain, want, "rendered %q", source)
			}
		}
	}
}
