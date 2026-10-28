package chat

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComposerGrowsForWrappedAndMultilineDraftsWithoutChangingText(t *testing.T) {
	m := &editorCmp{textarea: CreateTextArea(nil)}
	require.Equal(t, 2, m.DesiredHeight(80, 30))
	text := strings.Repeat("long message ", 40)
	m.textarea.SetValue(text)
	require.Greater(t, m.DesiredHeight(40, 30), 2)
	require.LessOrEqual(t, m.DesiredHeight(40, 30), 9)
	require.Equal(t, text, m.textarea.Value())
	m.textarea.SetValue("one\ntwo\nthree")
	require.Equal(t, 4, m.DesiredHeight(80, 30))
}

func TestComposerAttachmentRowsStayInsideAllocatedHeight(t *testing.T) {
	m := &editorCmp{textarea: CreateTextArea(nil), attachments: []message.Attachment{{FileName: "first-file.txt"}, {FileName: "second-file.txt"}, {FileName: "third-file.txt"}}}
	m.SetSize(36, 6)
	m.textarea.SetValue("one\ntwo")
	require.LessOrEqual(t, lipgloss.Width(m.View()), 36)
	require.LessOrEqual(t, lipgloss.Height(m.View()), 6)
	require.Equal(t, 36, m.width)
}

// Record long-conversation rendering cost separately from correctness tests.
// This exercises the production cache and viewport without provider requests.
func BenchmarkLongConversationViewport(b *testing.B) {
	m := NewMessagesCmp(&app.App{CoderAgent: benchmarkAgent{}}).(*messagesCmp)
	m.SetSize(100, 30)
	for i := 0; i < 1000; i++ {
		role := message.Assistant
		if i%2 == 0 {
			role = message.User
		}
		m.messages = append(m.messages, message.Message{ID: fmt.Sprint(i), Role: role, Parts: []message.ContentPart{message.TextContent{Text: "A bounded example message with source references and ordinary project information."}}})
	}
	m.renderView()
	m.viewport.GotoBottom()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.viewport.LineUp(1)
		_ = m.View()
		m.viewport.LineDown(1)
	}
}

type benchmarkAgent struct{ agent.Service }

func (benchmarkAgent) IsBusy() bool              { return false }
func (benchmarkAgent) IsSessionBusy(string) bool { return false }
