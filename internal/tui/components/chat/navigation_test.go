package chat

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestTranscriptDefaultsYieldToExistingGlobalShortcut(t *testing.T) {
	m := selectionFixture(t)
	previous := config.DefaultKeybindings["models"]
	t.Cleanup(func() { config.DefaultKeybindings["models"] = previous })
	for _, key := range []rune{'f', 's'} {
		config.DefaultKeybindings["models"] = "alt+" + string(key)
		require.False(t, m.TranscriptKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{key}}))
		require.False(t, m.find.open)
		require.False(t, m.selection.keyboard)
	}
	m.StartSearch("ação")
	require.NotEmpty(t, m.find.matches, "slash search remains available")
	m.ToggleKeyboardSelection()
	require.True(t, m.selection.keyboard, "slash selection remains available")
}

func TestKeyboardSelectionExtendsWholeUnicodeGraphemes(t *testing.T) {
	m := selectionFixture(t)
	m.StartSearch("ação")
	require.True(t, m.TranscriptKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'s'}}))
	for range 3 {
		require.True(t, m.TranscriptKey(tea.KeyMsg{Type: tea.KeyShiftRight}))
	}
	require.Equal(t, "ação", m.SelectedText())
	require.False(t, m.find.open)
	require.True(t, m.selection.keyboard)
	m.TranscriptKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.Empty(t, m.SelectedText())
	require.False(t, m.TranscriptKey(tea.KeyMsg{Type: tea.KeyLeft}), "ordinary arrows belong to the composer")
	m.StartSearch("漢字")
	m.ToggleKeyboardSelection()
	require.Equal(t, "漢", m.SelectedText())
	m.TranscriptKey(tea.KeyMsg{Type: tea.KeyShiftRight})
	require.Equal(t, "漢字", m.SelectedText())
	m.TranscriptKey(tea.KeyMsg{Type: tea.KeyLeft})
	require.Equal(t, "漢", m.SelectedText(), "plain arrow moves a single-cell selection")
}

func TestTranscriptSearchIsBoundedLiteralAndNavigable(t *testing.T) {
	m := selectionFixture(t)
	m.transcriptLines = []string{"[literal] first", "unrelated", "[literal] last"}
	m.viewport.SetContent(strings.Join(m.transcriptLines, "\n"))
	m.StartSearch("[LITERAL]")
	require.Len(t, m.find.matches, 2)
	require.Equal(t, 0, m.find.matches[m.find.selected].row)
	m.TranscriptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, 2, m.find.matches[m.find.selected].row)
	m.TranscriptKey(tea.KeyMsg{Type: tea.KeyUp})
	require.Equal(t, 0, m.find.matches[m.find.selected].row)
	require.Contains(t, ansi.Strip(m.View()), "1/2")
	m.TranscriptKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.True(t, m.find.open, "Ctrl+C in query focus cannot quit")
	m.TranscriptKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.False(t, m.find.open)
	m.transcriptLines = make([]string, 400)
	for i := range m.transcriptLines {
		m.transcriptLines[i] = "match"
	}
	m.StartSearch("match")
	require.Len(t, m.find.matches, 200)
	require.True(t, m.find.limited)
	m.TranscriptKey(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune(strings.Repeat("x", 400) + "\n")})
	require.LessOrEqual(t, len([]rune(m.find.query)), 128)
	require.NotContains(t, m.find.query, "\n")
}

func TestTranscriptSearchPreservesReadingDuringStream(t *testing.T) {
	m := scrollableMessages(t)
	m.StartSearch("history")
	offset := m.viewport.YOffset
	updated := m.messages[0]
	updated.AppendContent("new output\n")
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: updated})
	require.Equal(t, offset, m.viewport.YOffset)
	require.True(t, m.find.open)
	require.True(t, m.unread)
	m.SetSize(50, 14)
	require.True(t, m.find.open)
	require.NotEmpty(t, m.find.matches, "reflow recomputes search coordinates")
	m.Update(SessionClearedMsg{})
	require.False(t, m.find.open)
}
