package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func testTextKey(text string, mod tea.KeyMod) tea.KeyPressMsg {
	code := tea.KeyExtended
	if runes := []rune(text); len(runes) == 1 {
		code = runes[0]
	}
	return tea.KeyPressMsg{Code: code, Text: text, Mod: mod}
}

func TestNativePasteCannotAnswerProtectedModalsOrReachBackground(t *testing.T) {
	for name, open := range map[string]func(*appModel){
		"permission": func(a *appModel) { a.showPermissions = true },
		"quit":       func(a *appModel) { a.showQuit = true },
		"help":       func(a *appModel) { a.showHelp = true },
		"summary":    func(a *appModel) { a.isCompacting = true },
	} {
		t.Run(name, func(t *testing.T) {
			p := &inputPage{}
			a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]util.Model{page.ChatPage: p}}
			open(&a)
			require.Nil(t, a.pasteMessage(tea.PasteMsg{Content: "y\n/quit\nctrl+c"}))
			require.Empty(t, p.received)
		})
	}
}

func TestNativePasteReachesComposerAsOneLiteralEvent(t *testing.T) {
	p := &inputPage{}
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]util.Model{page.ChatPage: p}}
	pasted := tea.PasteMsg{Content: "y\n/quit\nctrl+c\n日本語"}
	require.Nil(t, a.pasteMessage(pasted))
	require.Equal(t, []tea.Msg{pasted}, p.received)
	require.False(t, a.showQuit)
}
