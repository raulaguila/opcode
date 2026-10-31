package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchedHTMLPreservesMarkdownStructure(t *testing.T) {
	const html = `<h2>Diagnóstico</h2><p>Leia o <a href="https://example.test/README.md#L2">arquivo</a> e use <code>Request()</code>.</p><pre><code class="language-go">fmt.Println("ação")
</code></pre>`
	response, err := formatFetchedContent("markdown", "text/html; charset=utf-8", html)
	require.NoError(t, err)
	require.False(t, response.IsError)
	for _, fragment := range []string{"## Diagnóstico", "[arquivo](https://example.test/README.md#L2)", "`Request()`", "```go\n", `fmt.Println("ação")`} {
		require.Contains(t, response.Content, fragment)
	}
	require.NotContains(t, response.Content, "<h2>")
	require.NotContains(t, response.Content, "<pre>")
}

func TestFetchedNonHTMLMarkdownRetainsLiteralCode(t *testing.T) {
	const content = "# literal\nvalue := `ação`\n"
	response, err := formatFetchedContent("markdown", "text/plain", content)
	require.NoError(t, err)
	require.Equal(t, "```\n"+content+"\n```", response.Content)
}
