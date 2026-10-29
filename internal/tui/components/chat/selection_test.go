package chat

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

func selectionFixture(t *testing.T) *messagesCmp {
	t.Helper()
	m := NewMessagesCmp(&app.App{}).(*messagesCmp)
	m.SetSize(80, 12)
	m.session = session.Session{ID: "s"}
	m.messages = []message.Message{{ID: "a", SessionID: "s", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "alpha ação 漢字 👩‍💻 omega\n\nsecond line"}}}}
	m.renderView()
	return m
}

func responsePoint(t *testing.T, m *messagesCmp, text string) textPoint {
	t.Helper()
	for row, line := range m.transcriptLines {
		plain := ansi.Strip(line)
		if at := strings.Index(plain, text); at >= 0 {
			return textPoint{ansi.StringWidth(plain[:at]), row}
		}
	}
	t.Fatalf("missing text %q", text)
	return textPoint{}
}

func selectDrag(t *testing.T, m *messagesCmp, from, to textPoint) {
	t.Helper()
	for _, step := range []struct {
		at     textPoint
		action tea.MouseAction
	}{
		{from, tea.MouseActionPress}, {to, tea.MouseActionMotion}, {to, tea.MouseActionRelease},
	} {
		_, cmd := m.Update(tea.MouseMsg{X: step.at.x + 1, Y: step.at.row - m.viewport.YOffset + 1, Button: tea.MouseButtonLeft, Action: step.action})
		require.Nil(t, cmd, "selection must never open response actions or copy automatically")
	}
}

func TestResponseClickDoesNotOpenActionsAndDragSelectsUnicode(t *testing.T) {
	m := selectionFixture(t)
	start := responsePoint(t, m, "ação")
	_, cmd := m.Update(tea.MouseMsg{X: start.x + 1, Y: start.row + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	require.Nil(t, cmd)
	m.Update(tea.MouseMsg{X: start.x + 1, Y: start.row + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	require.Empty(t, m.SelectedText(), "a click alone does not select or copy")
	end := responsePoint(t, m, " omega")
	end.x--
	before := m.View()
	selectDrag(t, m, start, end)
	require.Equal(t, "ação 漢字 👩‍💻", m.SelectedText())
	require.Equal(t, ansi.Strip(before), ansi.Strip(m.View()), "highlight must not alter visible content")
	require.Equal(t, lipgloss.Width(before), lipgloss.Width(m.View()))
	require.Equal(t, lipgloss.Height(before), lipgloss.Height(m.View()))
	selectDrag(t, m, end, start)
	require.Equal(t, "ação 漢字 👩‍💻", m.SelectedText(), "reverse drag selects the same graphemes")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	require.IsType(t, ResponseActionsMsg{}, cmd(), "actions remain an explicit keyboard operation")
}

func TestSelectionSnapsWideAndCombiningGraphemes(t *testing.T) {
	for _, text := range []string{"漢", "👩‍💻", "e\u0301"} {
		width := ansi.StringWidth(text)
		left, right := selectionCells("A"+text+"Z", 1+width-1, 1+width)
		require.Equal(t, text, ansi.Strip(ansi.Cut("A"+text+"Z", left, right)))
	}
}

func TestSelectionHighlightWithoutColors(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	m := selectionFixture(t)
	first := responsePoint(t, m, "alpha")
	before := m.View()
	selectDrag(t, m, first, textPoint{first.x + 4, first.row})
	require.Contains(t, m.View(), "\x1b[7malpha\x1b[27m", "monochrome terminals must still show selection")
	require.Equal(t, ansi.Strip(before), ansi.Strip(m.View()))
	require.Equal(t, "alpha", m.SelectedText())
}

func TestSelectionFreezesStreamingCellsAndClearsOnReflowOrSessionChange(t *testing.T) {
	m := selectionFixture(t)
	first, last := responsePoint(t, m, "alpha"), responsePoint(t, m, "second line")
	last.x += len("second line") - 1
	selectDrag(t, m, first, last)
	selected, before := m.SelectedText(), ansi.Strip(m.View())
	require.Contains(t, selected, "second line")
	updated := m.messages[0]
	updated.AppendContent("\n\nNEW_STREAM_OUTPUT")
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: updated})
	require.Equal(t, selected, m.SelectedText())
	require.Contains(t, ansi.Strip(m.View()), strings.Split(before, "\n")[1])
	require.NotContains(t, ansi.Strip(m.View()), "NEW_STREAM_OUTPUT")
	require.True(t, m.unread)
	m.Update(JumpToLatestMsg{})
	require.Empty(t, m.SelectedText())
	require.True(t, m.viewport.AtBottom())
	require.Contains(t, strings.Join(m.transcriptLines, "\n"), "NEW_STREAM_OUTPUT")
	m.viewport.GotoTop()
	selectDrag(t, m, first, textPoint{first.x + 4, first.row})
	require.True(t, m.HasSelection())
	m.SetSize(60, 12)
	require.Empty(t, m.SelectedText(), "reflow must not leave stale cell coordinates")
	first = responsePoint(t, m, "alpha")
	m.viewport.GotoTop()
	selectDrag(t, m, first, textPoint{first.x + 4, first.row})
	require.True(t, m.HasSelection())
	m.Update(SessionClearedMsg{})
	require.Empty(t, m.SelectedText())
}

func TestSelectionUsesScrolledCoordinatesAndClampsOutsideRelease(t *testing.T) {
	m := scrollableMessages(t)
	first := textPoint{3, m.viewport.YOffset + 1}
	selectDrag(t, m, first, textPoint{first.x + 5, first.row})
	require.NotEmpty(t, m.SelectedText())
	m.Update(tea.MouseMsg{X: 4, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 999, Y: 999, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	require.False(t, m.SelectionDragging())
	require.NotEmpty(t, m.SelectedText())
}
