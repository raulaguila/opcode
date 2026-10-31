package page

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/stretchr/testify/require"
)

type navigationProbe struct {
	selectionProbe
	query        string
	selectedKeys int
}

func (n *navigationProbe) TranscriptKey(msg tea.KeyPressMsg) bool {
	n.selectedKeys++
	return msg.Mod&tea.ModAlt != 0
}
func (n *navigationProbe) StartSearch(s string)     { n.query = s }
func (n *navigationProbe) ToggleKeyboardSelection() { n.selected = true }

func TestTranscriptNavigationPreservesComposerAndPTYKeys(t *testing.T) {
	p := workflowPage(t)
	probe := &navigationProbe{}
	p.transcript = probe
	p.editor.Update(chat.RestoreDraftMsg{Text: "draft preserved"})
	before := ansi.Strip(p.editor.View())
	_, handled := p.SelectionKey(tea.KeyPressMsg{Mod: tea.ModAlt, Code: 'f'})
	require.True(t, handled)
	require.Equal(t, before, ansi.Strip(p.editor.View()))
	p.workspaceCommand("search", "needle")
	require.Equal(t, "needle", probe.query)
	p.workspaceCommand("select", "")
	require.True(t, probe.selected)
	count := probe.selectedKeys
	p.terminal.focused = true
	_, handled = p.SelectionKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	require.False(t, handled)
	require.Equal(t, count, probe.selectedKeys)
}
