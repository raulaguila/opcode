package styles

import (
	"strings"
	"testing"
)

func BenchmarkAuditMarkdownManyBlocks(b *testing.B) {
	content := strings.Repeat("## Evidence\n\nNarrative with [source][ref], `identifier`, and ordinary explanation.\n\n4. first source\n5. second source\n\n", 20)
	content += "```go\nfmt.Println(\"wide code retains its terminal measure\")\n```\n\n[ref]: https://example.test/source#L1\n"
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		RenderMarkdownBlocks(content, 120, 48)
	}
}
