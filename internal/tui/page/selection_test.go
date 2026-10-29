package page

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

type selectionProbe struct {
	text     string
	dragging bool
	selected bool
}

func (s *selectionProbe) LatestResponse() string  { return s.text }
func (s *selectionProbe) SelectedText() string    { return s.text }
func (s *selectionProbe) HasSelection() bool      { return s.selected || s.text != "" }
func (s *selectionProbe) ClearSelection()         { s.text = ""; s.dragging = false; s.selected = false }
func (s *selectionProbe) SelectionDragging() bool { return s.dragging }

func TestSelectionCopyAndEscapeDoNotCancelAgent(t *testing.T) {
	p := workflowPage(t)
	selection := &selectionProbe{text: "only the selected text"}
	p.transcript = selection
	cmd, handled := p.SelectionKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.True(t, handled)
	require.Equal(t, "only the selected text", cmd().(util.CopyTextMsg).Text)
	require.NotEmpty(t, selection.text, "copy does not clear a highlight")
	cmd, handled = p.SelectionKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Empty(t, selection.text)
	_, handled = p.SelectionKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.False(t, handled, "ordinary quit behavior remains when nothing is selected")
	selection.selected = true
	cmd, handled = p.SelectionKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.True(t, handled, "whitespace-only selection cannot trigger quit")
	require.Nil(t, cmd, "do not copy empty text over the clipboard")
	_, handled = p.SelectionKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, handled)
	selection.text = "selected"
	p.terminal.focused = true
	_, handled = p.SelectionKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.False(t, handled, "Ctrl+C in a PTY must still interrupt the child")
	p.terminal.focused = false
	p.showWorkspace("actions", "Actions", "")
	_, handled = p.SelectionKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.False(t, handled, "focused panels retain Escape")
	require.Zero(t, p.app.CoderAgent.(*workflowProbe).canceled)
}

func TestSelectionCapturesReleaseOverHeaderAndOutsidePanel(t *testing.T) {
	for _, point := range [][2]int{{3, 0}, {999, 999}} {
		p := workflowPage(t)
		p.height = 24
		p.transcript = &selectionProbe{dragging: true}
		capture := &mouseMessages{}
		p.messages = capture
		p.Update(tea.MouseMsg{X: point[0], Y: point[1], Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
		require.Len(t, capture.received, 1)
		require.Equal(t, tea.MouseActionRelease, capture.received[0].(tea.MouseMsg).Action)
	}
}
