package page

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/util"
)

// SelectionKey is queried before global cancel/quit shortcuts. Modal dialogs,
// the terminal and the editor's own selection keep their existing precedence.
func (p *chatPage) SelectionKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if p.WorkspaceFocused() || p.terminal.focused || p.slashMenuOpen() || p.showCompletionDialog || p.workflow.question.ID != "" {
		return nil, false
	}
	selection, ok := p.transcript.(chat.SelectionControl)
	if navigation, ok := p.transcript.(chat.TranscriptNavigation); ok && navigation.TranscriptKey(msg) {
		return chat.PendingHistoryCommand(p.transcript), true
	}
	if !ok {
		return nil, false
	}
	if msg.String() == "esc" && (selection.SelectionDragging() || selection.HasSelection()) {
		selection.ClearSelection()
		return nil, true
	}
	if msg.String() == "ctrl+c" && selection.HasSelection() {
		if text := selection.SelectedText(); text != "" {
			return util.CmdHandler(util.CopyTextMsg{Text: text}), true
		}
		return nil, true // Whitespace-only selection must not trigger global Quit.
	}
	return nil, false
}
