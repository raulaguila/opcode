package styles

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInlineMathBecomesPlainSymbols(t *testing.T) {
	require.Equal(t, "Entrada → Saída, a ≤ b", plainInlineMath(`Entrada $\rightarrow$ Saída, $a \leq b$`))
	require.Equal(t, `custa $5 e $\frac{a}{b}$`, plainInlineMath(`custa $5 e $\frac{a}{b}$`), "unknown math stays as typed")
	require.Equal(t, "`$\\to$` e → e\n```\n$\\to$\n```\n", plainInlineMath("`$\\to$` e $\\to$ e\n```\n$\\to$\n```\n"), "code stays literal")
	require.Contains(t, RenderMarkdownBlocks(`a $\rightarrow$ b`, 40, 40), "a → b")
}
