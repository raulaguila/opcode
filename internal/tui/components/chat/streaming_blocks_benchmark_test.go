package chat

import (
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
)

// Same real Update path and bounded growth cycle on baseline and candidate.
func BenchmarkAuditChatGrowingBlocks(b *testing.B) {
	m := renderFixture(100)
	last := m.messages[len(m.messages)-1]
	prefix := strings.Repeat("Stable prose with cited source.go:12 and **explanation**.\n\n", 24)
	text := prefix
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if i%32 == 0 {
			text = prefix
		}
		text += " More streamed explanation."
		last.Parts = []message.ContentPart{message.TextContent{Text: text}}
		m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	}
}
