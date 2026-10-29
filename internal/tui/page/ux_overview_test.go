package page

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRecoveryUsesActionableCopyAndKeepsTechnicalDetails(t *testing.T) {
	p, _ := workspacePage(t)
	for _, tc := range []struct{ raw, title, action string }{
		{"context budget exceeded: secret diagnostic", config.Text("Limite do modelo atingido", "Model limit reached"), "/context"},
		{"out of memory: diagnostic", config.Text("Memória insuficiente no servidor", "Server memory exhausted"), "/models"},
		{"connection refused: diagnostic", config.Text("Provedor indisponível", "Provider unavailable"), "/retry"},
		{"context deadline exceeded", config.Text("Provedor indisponível", "Provider unavailable"), "/retry"},
		{"context canceled", config.Text("Execução cancelada", "Run cancelled"), "/retry"},
		{"401 unauthorized: diagnostic", config.Text("Autenticação necessária", "Authentication required"), "/settings"},
	} {
		p.recovery = recoveryState{err: errors.New(tc.raw)}
		view := p.composerPanel()
		require.Contains(t, view, tc.title)
		require.Contains(t, view, tc.action)
		require.NotContains(t, view, tc.raw)
		p.workspaceCommand("error", "")
		require.Contains(t, p.workspace.body, tc.raw)
		p.closeWorkspace()
	}
	p.recovery.resolved = true
	require.Zero(t, p.recoveryHeight())
}
func TestTaskOverviewAndContextTabsPreserveDraftAndDoNotCompact(t *testing.T) {
	p, a := workspacePage(t)
	p.openTask()
	require.Equal(t, "task", p.workspace.kind)
	require.Len(t, p.workspace.actions, 7)
	cmd := p.openContext(false)
	p.workspaceResult(cmd().(WorkspaceResultMsg))
	require.Contains(t, p.workspaceView(), config.Text("1 Orçamento", "1 Budget"))
	require.Zero(t, a.compact)
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.False(t, p.WorkspaceFocused())
}
