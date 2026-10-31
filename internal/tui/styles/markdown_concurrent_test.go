package styles

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConcurrentMarkdownRequestsKeepReferencesIsolated(t *testing.T) {
	const count = 16
	sources, expected := make([]string, count), make([]string, count)
	for i := range count {
		sources[i] = fmt.Sprintf("# Heading %d\n\n[local][ref] and footnote[^1].\n\n- **Nested** `code`\n\n[ref]: https://example.test/%d\n\n[^1]: Note %d\n\n```go\nfmt.Println(%d)\n```\n", i, i, i, i)
		expected[i] = markdownBlocksReference(sources[i], 90, 60)
	}
	results := make([]string, count)
	var done sync.WaitGroup
	for i := range count {
		done.Go(func() { results[i] = RenderMarkdownBlocks(sources[i], 90, 60) })
	}
	done.Wait()
	for i := range count {
		require.Equal(t, expected[i], results[i], "request %d must keep its own references, footnotes and AST", i)
	}
}
