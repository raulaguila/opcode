package chat

import (
	"charm.land/lipgloss/v2"
	"fmt"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestComposerGrowsForWrappedAndMultilineDraftsWithoutChangingText(t *testing.T) {
	m := &editorCmp{textarea: CreateTextArea(nil)}
	require.Equal(t, 4, m.DesiredHeight(80, 30), "reserve three writing rows plus the border")
	text := strings.Repeat("long message ", 40)
	m.textarea.SetValue(text)
	require.Greater(t, m.DesiredHeight(40, 30), 4)
	require.LessOrEqual(t, m.DesiredHeight(40, 30), 9)
	require.Equal(t, text, m.textarea.Value())
	m.textarea.SetValue("one\ntwo\nthree")
	require.Equal(t, 4, m.DesiredHeight(80, 30))
	m.textarea.SetValue("one\ntwo\nthree\nfour")
	require.Equal(t, 5, m.DesiredHeight(80, 30))
}

func TestComposerKeepsWritingSpaceWhenEmptyOrAttachedAndAdaptsToShortTerminal(t *testing.T) {
	m := &editorCmp{textarea: CreateTextArea(nil)}
	m.SetSize(80, m.DesiredHeight(80, 24)-1)
	require.Equal(t, 3, m.textarea.Height())
	require.Equal(t, 3, lipgloss.Height(m.View()))
	m.attachments = []message.Attachment{{FileName: "note.txt"}}
	m.SetSize(80, m.DesiredHeight(80, 24)-1)
	require.Equal(t, 3, m.textarea.Height(), "attachment rows must not consume the minimum writing area")
	require.Equal(t, 3, m.DesiredHeight(80, 9), "a short terminal can reduce the minimum to preserve chat space")
}

func TestComposerAttachmentRowsStayInsideAllocatedHeight(t *testing.T) {
	m := &editorCmp{textarea: CreateTextArea(nil), attachments: []message.Attachment{{FileName: "first-file.txt"}, {FileName: "second-file.txt"}, {FileName: "third-file.txt"}}}
	m.SetSize(36, 6)
	m.textarea.SetValue("one\ntwo")
	require.LessOrEqual(t, lipgloss.Width(m.View()), 36)
	require.LessOrEqual(t, lipgloss.Height(m.View()), 6)
	require.Equal(t, 36, m.width)
}

func TestComposerCanUseEightRowsAtOrdinaryTerminalHeights(t *testing.T) {
	m := &editorCmp{textarea: CreateTextArea(nil)}
	draft := strings.Repeat("ação 漢字\n", 7) + "última linha"
	m.textarea.SetValue(draft)
	for _, height := range []int{18, 24, 40} {
		allocated := m.DesiredHeight(80, height)
		require.Equal(t, 9, allocated)
		m.SetSize(80, allocated-1)
		require.Equal(t, 8, m.textarea.Height())
		require.Contains(t, m.View(), "última linha")
		require.Equal(t, draft, m.textarea.Value())
	}
}

func TestAttachmentRenderingIsBoundedAndUnicodeSafe(t *testing.T) {
	m := &editorCmp{textarea: CreateTextArea(nil)}
	for range 30 {
		m.attachments = append(m.attachments, message.Attachment{FileName: "ação-漢字-um-arquivo-muito-longo.txt"})
	}
	for _, width := range []int{20, 36, 80} {
		view := m.attachmentsContentWidth(width)
		require.LessOrEqual(t, lipgloss.Height(view), 2)
		require.LessOrEqual(t, lipgloss.Width(view), width)
		require.True(t, utf8.ValidString(view))
	}
	allocated := m.DesiredHeight(36, 24)
	m.SetSize(36, allocated-1)
	require.Equal(t, 3, m.textarea.Height())
	require.Len(t, m.attachments, 30, "summarizing chips must not drop attachments")
	m.textarea.SetValue(strings.Repeat("line\n", 7) + "end")
	for width := 20; width <= 100; width++ {
		allocated := m.DesiredHeight(width, 30)
		m.SetSize(width, allocated-1)
		require.Equal(t, 8, m.textarea.Height(), "attachment measurement must use the allocated panel width")
	}
}

func TestComposerFocusChangesPreserveTextAndHideCursor(t *testing.T) {
	m := &editorCmp{textarea: CreateTextArea(nil)}
	m.textarea.SetValue("rascunho ação 漢字")
	m.SetInputFocused(false)
	require.False(t, m.textarea.Focused())
	m.SetInputFocused(true)
	require.True(t, m.textarea.Focused())
	require.Equal(t, "rascunho ação 漢字", m.textarea.Value())
}

// Record long-conversation rendering cost separately from correctness tests.
// This exercises the production cache and viewport without provider requests.
func BenchmarkLongConversationViewport(b *testing.B) {
	m := NewMessagesCmp(&app.App{CoderAgent: benchmarkAgent{}}).(*messagesCmp)
	m.SetSize(100, 30)
	for i := range 1000 {
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
		m.viewport.ScrollUp(1)
		_ = m.View()
		m.viewport.ScrollDown(1)
	}
}

type benchmarkAgent struct{ agent.Service }

func (benchmarkAgent) IsBusy() bool              { return false }
func (benchmarkAgent) IsSessionBusy(string) bool { return false }
