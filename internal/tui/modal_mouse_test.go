package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/stretchr/testify/require"
)

type modalTestPage struct {
	inputPage
	body string
}

func (p *modalTestPage) View() string { return p.body }

func TestPermissionClickMatchesRenderedRootAtEverySize(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {120, 36}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			status, _ := core.NewStatusCmp(nil).Update(tea.WindowSizeMsg{Width: size[0]})
			approval := dialog.NewPermissionDialogCmp()
			approval.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1] - 1})
			request := permission.PermissionRequest{ID: "exact-request", ToolName: "test_lsp_connection", Path: "/workspace/" + strings.Repeat("nested/", 15), Params: map[string]string{"command": "/bin/bash"}}
			approval.SetPermissions(request)
			a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: &modalTestPage{body: strings.Repeat(" \n", size[1]-2)}}, status: status.(core.StatusCmp), width: size[0], height: size[1] - 1, permissions: approval, showPermissions: true}
			frame := ansi.Strip(a.View())
			found := false
			for y, line := range strings.Split(frame, "\n") {
				if before, _, ok := strings.Cut(line, "Allow (a)"); ok {
					_, cmd := a.Update(tea.MouseMsg{X: ansi.StringWidth(before), Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
					require.NotNil(t, cmd, "visible approval target must be clickable:\n%s", frame)
					response := cmd().(dialog.PermissionResponseMsg)
					require.Equal(t, dialog.PermissionAllow, response.Action)
					require.Equal(t, request, response.Permission)
					found = true
				}
			}
			require.True(t, found, "approval must remain visible:\n%s", frame)
		})
	}
}

func TestQuitMouseDoesNotReplaceThemeDialog(t *testing.T) {
	status, _ := core.NewStatusCmp(nil).Update(tea.WindowSizeMsg{Width: 80})
	quit := dialog.NewQuitCmp()
	themes := dialog.NewThemeDialogCmp()
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: &inputPage{}}, status: status.(core.StatusCmp), width: 80, height: 23, quit: quit, themeDialog: themes, showQuit: true}
	for y, line := range strings.Split(ansi.Strip(a.View()), "\n") {
		if before, _, ok := strings.Cut(line, "Continue"); ok {
			u, cmd := a.Update(tea.MouseMsg{X: ansi.StringWidth(before), Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			after := u.(appModel)
			require.Same(t, themes, after.themeDialog)
			require.Same(t, quit, after.quit)
			require.NotNil(t, cmd)
			require.IsType(t, dialog.CloseQuitMsg{}, cmd())
			return
		}
	}
	t.Fatal("Continue button missing")
}

func TestSSHPasteNeverInvokesServerClipboard(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "fixture")
	p := &inputPage{}
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: p}}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	require.NotNil(t, cmd)
	require.Empty(t, p.received)
}
