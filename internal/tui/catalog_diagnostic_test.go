package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestCatalogNoticeReachesErrorPanelWithoutStartingWork(t *testing.T) {
	emptySetupConfig(t)
	p := page.NewChatPage(&app.App{CoderAgent: &terminalAgent{}})
	p.(interface{ SetSize(int, int) tea.Cmd }).SetSize(100, 30)
	m := appModel{pages: map[page.PageID]util.Model{page.ChatPage: p}, status: core.NewStatusCmp(nil)}
	const details = "TLS: check certificate and trust"
	_, _, handled := m.routeStatus(util.InfoMsg{Type: util.InfoTypeError, Msg: "Check connection /error", Details: details})
	require.True(t, handled)
	_, cmd := m.pages[page.ChatPage].Update(page.WorkspaceCommandMsg{Command: "error"})
	require.Nil(t, cmd, "opening a catalog diagnostic never retries inference or a command")
	view := ansi.Strip(m.pages[page.ChatPage].View())
	require.Contains(t, view, details)
	require.Contains(t, view, "Connection details")
	require.NotContains(t, view, "Run interrupted")
}
