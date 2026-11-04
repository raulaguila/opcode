package ui

import (
	"encoding/base64"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestChatSelectionKeepsWholeGraphemesInBothDirections(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		m := New(nil)
		m.rows = []string{"ab界👩‍💻e\u0301fim   ", "segunda linha   "}
		a, b := chatPoint{3, 0}, chatPoint{6, 0}
		if reverse {
			a, b = b, a
		}
		m.selection = &chatSelection{anchor: a, focus: b, moved: true}
		require.Equal(t, "界👩‍💻e\u0301", m.selectedText())
		require.Contains(t, strings.Join(m.highlightSelection(m.rows, 0), "\n"), "\x1b[7m界👩‍💻e\u0301\x1b[27m")
		m.selection = &chatSelection{anchor: chatPoint{2, 0}, focus: chatPoint{6, 1}, moved: true}
		require.Equal(t, "界👩‍💻e\u0301fim\nsegunda", m.selectedText())
	}
}

func TestDragSelectsScrolledChatAndCtrlCCopiesWithoutQuitting(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "test")
	m := New(nil)
	m.width, m.height = 60, 24
	m.session.ID = "s"
	m.composer.SetValue("rascunho")
	m.rows = []string{"ignored", "first", "second", "third", "fourth"}
	m.transcript.SetHeight(2)
	m.transcript.SetContentLines([]string{"", "", "", "", ""})
	m.transcript.SetYOffset(1)
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: headerRows})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 3, Y: headerRows + 1})
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 3, Y: headerRows + 1})
	require.Equal(t, "first\nseco", m.selectedText())
	cmd := m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	require.NotNil(t, cmd)
	result := cmd().(chatCopyMsg)
	require.NoError(t, result.err)
	require.Equal(t, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte("first\nseco"))+"\x07", result.sequence)
	require.Nil(t, m.dialog, "copy must not open quit confirmation")
	_, copyCmd := m.Update(result)
	require.IsType(t, tea.RawMsg{}, copyCmd())
	require.Equal(t, "rascunho", m.composer.Value())
	m.busy = true
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.Nil(t, m.selection)
	require.True(t, m.busy, "Esc first clears the selection")
}

func TestSelectionStaysStableWhileMessagesStreamAndClearsOnResize(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.session.ID = "s"
	m.messages = []message.Message{{ID: "a", SessionID: "s", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "original"}}}}
	m.layout()
	m.selection = &chatSelection{anchor: chatPoint{0, 1}, focus: chatPoint{20, 1}, session: "s", moved: true}
	selected := m.selectedText()
	require.NotEmpty(t, selected)
	updated := message.Message{ID: "a", SessionID: "s", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "updated"}}}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: updated})
	require.Contains(t, ansi.Strip(strings.Join(m.rows, "\n")), "updated", "a selection never freezes the transcript")
	require.Equal(t, selected, m.selectedText(), "the selected text survives the rows under it")
	require.NotContains(t, strings.Join(m.highlightSelection(m.rows, 0), ""), "\x1b[7m")
	m.selection = &chatSelection{session: "s", moved: true}
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	require.Nil(t, m.selection)
	m.selection = &chatSelection{session: "s", moved: true}
	m.session.ID = "another"
	m.refresh()
	require.Nil(t, m.selection)
}

func TestSelectionReleaseOutsideTranscriptIsBoundedAndClipboardRejectsRawEscapes(t *testing.T) {
	m := New(nil)
	m.width, m.height = 60, 24
	m.rows = []string{"alpha", "omega"}
	m.transcript.SetHeight(2)
	m.transcript.SetContentLines([]string{"", ""})
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: headerRows})
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 999, Y: 999})
	require.Equal(t, "alpha\nomega", m.selectedText())
	require.False(t, m.selection.dragging)
	require.Nil(t, m.copiedChat(chatCopyMsg{sequence: "\x1b]0;unsafe\x07"}))
}
