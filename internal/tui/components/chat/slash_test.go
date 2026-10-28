package chat

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func editorKey(m *editorCmp, key tea.KeyMsg) tea.Cmd { _, cmd := m.Update(key); return cmd }
func enterText(m *editorCmp, text string) {
	editorKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}
func slashMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, child := range batch {
			messages = append(messages, slashMessages(child)...)
		}
		return messages
	}
	return []tea.Msg{msg}
}
func slashEditor() *editorCmp { m := NewEditorCmp(&app.App{}).(*editorCmp); m.SetSize(80, 4); return m }

func TestSlashOpensOptionsFiltersAndCompletesWithoutSending(t *testing.T) {
	m := slashEditor()
	enterText(m, "/")
	require.True(t, m.SlashMenuOpen())
	view := m.SlashMenuView(80, 10)
	for _, command := range []string{"/mode", "/plan", "/task", "/resume"} {
		require.Contains(t, view, command)
	}
	enterText(m, "p")
	require.Contains(t, m.SlashMenuView(80, 10), "/plan")
	require.NotContains(t, m.SlashMenuView(80, 10), "/resume")
	require.Nil(t, editorKey(m, tea.KeyMsg{Type: tea.KeyTab}))
	require.Equal(t, "/plan", m.textarea.Value())
	require.False(t, m.SlashMenuOpen())
	require.False(t, m.submitting)
	messages := slashMessages(editorKey(m, tea.KeyMsg{Type: tea.KeyEnter}))
	require.Len(t, messages, 1)
	require.Equal(t, "/plan", messages[0].(SendMsg).Text)
}
func TestSlashModeSelectionAndAttachmentPreservationOnRejection(t *testing.T) {
	m := slashEditor()
	m.attachments = []message.Attachment{{FileName: "file.png"}}
	enterText(m, "/")
	require.Nil(t, editorKey(m, tea.KeyMsg{Type: tea.KeyEnter}))
	require.Equal(t, "/mode ", m.textarea.Value())
	require.False(t, m.submitting)
	view := m.SlashMenuView(80, 10)
	require.Contains(t, view, "/mode ask")
	require.Contains(t, view, "/mode plan")
	require.Contains(t, view, "/mode execute")
	editorKey(m, tea.KeyMsg{Type: tea.KeyDown})
	messages := slashMessages(editorKey(m, tea.KeyMsg{Type: tea.KeyEnter}))
	require.Len(t, messages, 1)
	require.Equal(t, "/mode plan", messages[0].(SendMsg).Text)
	require.Len(t, messages[0].(SendMsg).Attachments, 1)
	m.Update(SubmissionResultMsg{Text: "/mode plan", Error: errors.New("agent is busy")})
	require.Equal(t, "/mode plan", m.textarea.Value())
	require.Len(t, m.attachments, 1)
	m.Update(SubmissionResultMsg{Text: "/mode plan"})
	require.Empty(t, m.textarea.Value())
	require.Empty(t, m.attachments)
}

func TestSlashEscapeKeepsDraftAndEditingReopensSuggestions(t *testing.T) {
	m := slashEditor()
	enterText(m, "/mode p")
	require.True(t, m.SlashMenuOpen())
	require.Nil(t, editorKey(m, tea.KeyMsg{Type: tea.KeyEsc}))
	require.Equal(t, "/mode p", m.textarea.Value())
	require.False(t, m.SlashMenuOpen())
	require.False(t, m.submitting)
	enterText(m, "l")
	require.True(t, m.SlashMenuOpen())
	require.Contains(t, m.SlashMenuView(80, 10), "/mode plan")
}
func TestSlashDoesNotCapturePathsMultilineOrEditingInMiddle(t *testing.T) {
	for _, text := range []string{"Explique /mode", "/tmp/file.go", "linha\n/plan", "/tmp arquivo", ""} {
		m := slashEditor()
		m.textarea.SetValue(text)
		require.False(t, m.SlashMenuOpen(), text)
	}
	m := slashEditor()
	enterText(m, "/mode ask")
	editorKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	require.False(t, m.SlashMenuOpen())
}
func TestSlashMenuFitsSmallTerminalsAndUnknownPrefixKeepsNormalSend(t *testing.T) {
	m := slashEditor()
	enterText(m, "/")
	for _, size := range [][2]int{{80, 10}, {25, 4}, {12, 2}, {1, 1}} {
		view := m.SlashMenuView(size[0], size[1])
		require.LessOrEqual(t, lipgloss.Width(view), size[0])
		require.LessOrEqual(t, lipgloss.Height(view), size[1])
	}
	enterText(m, "desconhecido")
	require.Contains(t, m.SlashMenuView(80, 10), "Nenhum comando")
	messages := slashMessages(editorKey(m, tea.KeyMsg{Type: tea.KeyEnter}))
	require.Equal(t, "/desconhecido", messages[0].(SendMsg).Text)
}
