package message

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/memoryindex"
)

// Exercise the message-owned projection without timing authoritative history
// writes. This fixture also compiles before the optional owner Close exists.
func BenchmarkConversationProjection(b *testing.B) {
	ctx := context.Background()
	directory := b.TempDir()
	project := "/conversation-projection-benchmark"
	s := &service{}
	WithSearchIndex(directory, project, func(text string) string {
		return strings.ReplaceAll(text, "private-token", "[redacted]")
	})(s)
	b.Cleanup(func() {
		if closer, ok := any(s).(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				b.Error(err)
			}
		}
	})
	body := strings.Repeat("Conversation projection search evidence private-token. ", 16)
	msg := Message{ID: "hot", SessionID: "session", Role: User, UpdatedAt: 1}
	marker := "projectionwarmup"
	msg.Parts = []ContentPart{TextContent{Text: body + marker}}
	s.projectMessage(ctx, msg)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		marker = fmt.Sprintf("projectioniteration%d", i)
		msg.Parts[0] = TextContent{Text: body + marker}
		msg.UpdatedAt = int64(i + 2)
		s.projectMessage(ctx, msg)
	}
	b.StopTimer()
	// Projection errors are intentionally logged, so verify that the final
	// timed update committed rather than silently benchmarking a failed write.
	items, more, err := memoryindex.Search(ctx, directory, project, "conversation", "session", marker, 0, 1)
	if err != nil {
		b.Fatal(err)
	}
	if more || len(items) != 1 || items[0].ID != "message:hot" || !strings.Contains(items[0].Text, marker) {
		b.Fatalf("final projection update missing: items=%v more=%v", items, more)
	}
}
