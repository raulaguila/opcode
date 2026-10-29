package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

type focusPageProbe struct {
	inputPage
	focused bool
	content string
}

func (p *focusPageProbe) SetInputFocused(value bool) tea.Cmd { p.focused = value; return nil }
func (p *focusPageProbe) View() string                       { return p.content }

func TestEveryGlobalModalAndWindowBlurSuspendComposerFocus(t *testing.T) {
	for _, modal := range []func(*appModel){
		func(a *appModel) { a.showPermissions = true }, func(a *appModel) { a.showHelp = true },
		func(a *appModel) { a.showQuit = true }, func(a *appModel) { a.showSessionDialog = true },
		func(a *appModel) { a.showCommandDialog = true }, func(a *appModel) { a.showModelDialog = true },
		func(a *appModel) { a.showProviderDialog = true }, func(a *appModel) { a.showIntegrationDialog = true },
		func(a *appModel) { a.showInitDialog = true }, func(a *appModel) { a.showFilepicker = true },
		func(a *appModel) { a.showThemeDialog = true }, func(a *appModel) { a.showMultiArgumentsDialog = true },
		func(a *appModel) { a.isCompacting = true }, func(a *appModel) { a.unfocused = true },
	} {
		p := &focusPageProbe{}
		a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: p}}
		a.syncInputFocus()
		require.True(t, p.focused)
		modal(&a)
		a.syncInputFocus()
		require.False(t, p.focused)
	}
	p := &focusPageProbe{}
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: p}}
	updated, _ := a.Update(tea.BlurMsg{})
	a = updated.(appModel)
	require.False(t, p.focused)
	updated, _ = a.Update(tea.FocusMsg{})
	a = updated.(appModel)
	require.True(t, p.focused)
	a.showModelDialog = true
	a.modelDialog = dialog.NewModelDialogCmp()
	updated, _ = a.Update(dialog.CloseModelDialogMsg{})
	a = updated.(appModel)
	require.True(t, p.focused)
}

func TestRootFrameBoundsPreserveOneFooterDuringOverflowAndResize(t *testing.T) {
	p := &focusPageProbe{content: strings.Repeat(strings.Repeat("x", 250)+"\n", 100)}
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: p}}
	for _, size := range [][2]int{{180, 40}, {80, 24}, {36, 12}, {12, 4}, {8, 1}} {
		a.width, a.height = size[0], size[1]-1
		status, _ := core.NewStatusCmp(nil).Update(tea.WindowSizeMsg{Width: size[0], Height: size[1] - 1})
		a.status = status.(core.StatusCmp)
		view := a.View()
		require.LessOrEqual(t, lipgloss.Width(view), size[0])
		require.Equal(t, size[1], lipgloss.Height(view))
		footer := a.statusView()
		rows := strings.Split(view, "\n")
		require.Equal(t, footer, rows[len(rows)-1], "the footer must survive oversized content")
	}
}
