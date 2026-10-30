package page

import (
	"testing"

	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/stretchr/testify/require"
)

func TestQuitSlashIsHandledLocally(t *testing.T) {
	p := workflowPage(t)
	command, handled := p.handleWorkspaceMessage("/quit", nil)
	require.True(t, handled)
	opened := false
	for _, msg := range flattenCommands(command) {
		if _, ok := msg.(dialog.OpenQuitMsg); ok {
			opened = true
		}
	}
	require.True(t, opened)
	require.Zero(t, p.app.CoderAgent.(*workflowProbe).canceled)
}
