package chat

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestApplicationRepairStaysInsideProcess(t *testing.T) {
	m := NewMessagesCmp(&app.App{}).(*messagesCmp)
	m.SetSize(100, 40)
	m.messages = []message.Message{
		{ID: "u", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Explain project"}}},
		{ID: "old", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "invalid answer"}}},
		{ID: "repair", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Application validation: cite evidence", Origin: "application"}}},
		{ID: "a", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Verified answer main.go:1"}}},
	}
	require.Len(t, m.groups(), 1)
	m.renderView()
	require.NotContains(t, ansi.Strip(m.viewport.View()), "Application validation:")
	require.NotContains(t, ansi.Strip(m.viewport.View()), "invalid answer")
	require.Contains(t, ansi.Strip(m.viewport.View()), "Verified answer")
	m.Update(ToggleProcessMsg{})
	require.Contains(t, ansi.Strip(m.viewport.View()), "Application validation: cite evidence")
	m.messages[2].Parts = []message.ContentPart{message.TextContent{Text: "Application validation: user typed this"}}
	require.Len(t, m.groups(), 2, "a text prefix is not trusted origin metadata")
}
func TestEditorOwnsEditingKeysAndIgnoresConversationKeys(t *testing.T) {
	m := NewEditorCmp(&app.App{}).(*editorCmp)
	m.textarea.SetValue("draft")
	m.textarea.CursorEnd()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	require.Empty(t, m.textarea.Value())
	m.textarea.SetValue("first\nlast")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	require.Equal(t, "first\nlast!", m.textarea.Value())
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEnd, Alt: true}, {Type: tea.KeyPgUp, Alt: true}, {Type: tea.KeyPgDown, Alt: true}} {
		m.Update(k)
	}
	require.Equal(t, "first\nlast!", m.textarea.Value())
}

func TestProseUsesReadingMeasureAndCodeRetainsWidth(t *testing.T) {
	prose := strings.Repeat("readable words ", 40)
	view := ansi.Strip(renderMessage(prose, false, false, 160))
	for _, line := range strings.Split(view, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(strings.TrimRight(line, " ")), 76)
	}
	code := "```\n" + strings.Repeat("x", 100) + "\n```"
	require.Contains(t, ansi.Strip(renderMessage(code, false, false, 160)), strings.Repeat("x", 100))
}
