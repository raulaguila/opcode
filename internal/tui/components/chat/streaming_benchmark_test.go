package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
)

// Each measured event grows the same reply; the bounded cycle keeps the source
// sizes identical across benchmark calibrations and before/after binaries.
func BenchmarkAuditChatGrowingReply(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			benchmarkGrowingReply(b, renderFixture(count))
		})
	}
}

func benchmarkGrowingReply(b *testing.B, m *messagesCmp) {
	last := m.messages[len(m.messages)-1]
	prefix := strings.Repeat("A growing reply with cited repository evidence. ", 80)
	chunk := strings.Repeat(" streamed evidence", 8)
	text := prefix
	m.viewport.GotoBottom()
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		if index%32 == 0 {
			text = prefix
		}
		text += chunk
		last.Parts = []message.ContentPart{message.TextContent{Text: text}}
		m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	}
}

func BenchmarkAuditChatCacheOverflow(b *testing.B) {
	m := renderFixture(320)
	text := strings.Repeat("Historical source evidence remains available for selection and search. ", 240)
	for index := range m.messages {
		m.messages[index].Parts = []message.ContentPart{message.TextContent{Text: text}}
	}
	m.rerender()
	benchmarkGrowingReply(b, m)
}
