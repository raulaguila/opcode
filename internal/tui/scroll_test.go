package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/stretchr/testify/require"
)

func TestMouseDoesNotScrollBehindModal(t *testing.T) {
	for name, open := range map[string]func(*appModel){
		"permissions": func(a *appModel) { a.showPermissions = true },
		"help":        func(a *appModel) { a.showHelp = true },
		"quit":        func(a *appModel) { a.showQuit = true },
		"sessions":    func(a *appModel) { a.showSessionDialog = true },
		"commands":    func(a *appModel) { a.showCommandDialog = true },
		"models":      func(a *appModel) { a.showModelDialog = true },
		"init":        func(a *appModel) { a.showInitDialog = true },
		"files":       func(a *appModel) { a.showFilepicker = true },
		"theme":       func(a *appModel) { a.showThemeDialog = true },
		"arguments":   func(a *appModel) { a.showMultiArgumentsDialog = true },
		"summary":     func(a *appModel) { a.isCompacting = true },
	} {
		t.Run(name, func(t *testing.T) {
			a := appModel{}
			open(&a)
			// No page or dialog is initialized. Forwarding this mouse event to
			// either would panic rather than leaving the background untouched.
			_, cmd := a.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
			require.Nil(t, cmd)
		})
	}
}

type focusedTerminalPage struct {
	tea.Model
	received []tea.Msg
}

func (*focusedTerminalPage) TerminalFocused() bool { return true }
func (p *focusedTerminalPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	p.received = append(p.received, msg)
	return p, nil
}

func TestFocusedTerminalReceivesControlKeysBeforeGlobalShortcuts(t *testing.T) {
	p := &focusedTerminalPage{}
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: p}}
	updated, _ := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.False(t, updated.(appModel).showQuit)
	require.Len(t, p.received, 1)
	require.Equal(t, tea.KeyCtrlC, p.received[0].(tea.KeyMsg).Type)
}

type busyInputAgent struct{ agent.Service }

func (*busyInputAgent) IsBusy() bool { return true }

type inputPage struct{ received []tea.Msg }

func (*inputPage) Init() tea.Cmd { return nil }
func (*inputPage) View() string  { return "" }
func (p *inputPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	p.received = append(p.received, msg)
	return p, nil
}

func TestQuestionMarkReachesEditorWhileAgentWorks(t *testing.T) {
	p := &inputPage{}
	a := appModel{app: &app.App{CoderAgent: &busyInputAgent{}}, currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: p}, status: core.NewStatusCmp(nil)}
	updated, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	require.False(t, updated.(appModel).showHelp)
	require.Len(t, p.received, 1)
	require.Equal(t, "?", p.received[0].(tea.KeyMsg).String())
}
