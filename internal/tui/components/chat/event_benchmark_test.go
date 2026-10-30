package chat

import (
	"fmt"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"testing"
)

// Identical fixture for before/after comparisons: every iteration delivers a
// changed assistant snapshot through Update, rather than measuring cache hits.
func BenchmarkAuditChatEvent(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) { auditChatEvent(b, n) })
	}
}
func auditChatEvent(b *testing.B, n int) {
	m := NewMessagesCmp(&app.App{CoderAgent: benchmarkAgent{}}).(*messagesCmp)
	m.SetSize(100, 30)
	for i := 0; i < n; i++ {
		role := message.Assistant
		if i%2 == 0 {
			role = message.User
		}
		m.messages = append(m.messages, message.Message{ID: fmt.Sprint(i), Role: role, Parts: []message.ContentPart{message.TextContent{Text: "A bounded example message with source references and ordinary project information."}}})
	}
	m.renderView()
	m.viewport.GotoBottom()
	event := pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: m.messages[n-1]}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text := "A bounded example message with source references and ordinary project information."
		if i%2 == 0 {
			text += "!"
		}
		event.Payload.Parts = []message.ContentPart{message.TextContent{Text: text}}
		_, _ = m.Update(event)
	}
}
