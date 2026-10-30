package chat

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
)

// Include command execution: measure a displayed initial conversation, not just
// the cheaper act of scheduling its asynchronous persistence read.
func BenchmarkAuditChatSessionLoad(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			history := &renderHistory{histories: map[string][]message.Message{"A": benchmarkSessionMessages(count)}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
				m.SetSize(100, 30)
				runSessionLoadCommand(m, m.SetSession(session.Session{ID: "A"}))
			}
		})
	}
}

func benchmarkSessionMessages(count int) []message.Message {
	messages := make([]message.Message, count)
	for index := range messages {
		role := message.Assistant
		if index%2 == 0 {
			role = message.User
		}
		messages[index] = message.Message{ID: fmt.Sprint(index), SessionID: "A", Role: role, Parts: []message.ContentPart{message.TextContent{Text: "Historical evidence with [source](https://example.test/source) and ordinary project information."}}}
	}
	return messages
}

func runSessionLoadCommand(m *messagesCmp, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch result := cmd().(type) {
	case tea.BatchMsg:
		for _, next := range result {
			runSessionLoadCommand(m, next)
		}
	default:
		m.Update(result)
	}
}
