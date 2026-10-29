package dialog

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestApprovalResetsConsentAndSupportsEscapeAndExactMouseTargets(t *testing.T) {
	settingsDialog(t)
	for _, width := range []int{60, 80, 120} {
		m := NewPermissionDialogCmp().(*permissionDialogCmp)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		request := permission.PermissionRequest{ID: "first", ToolName: "test", Path: "/project", Params: map[string]string{"action": "exact"}}
		m.SetPermissions(request)
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		require.Equal(t, PermissionDeny, cmd().(PermissionResponseMsg).Action)
		m.selectedOption = 1
		request.ID = "second"
		m.SetPermissions(request)
		require.Equal(t, 2, m.selectedOption)
		_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		require.Equal(t, PermissionDeny, cmd().(PermissionResponseMsg).Action)
		// Use the actual rendered label, not the component's hit rectangles.
		view := ansi.Strip(m.View())
		clicked := false
		for y, line := range strings.Split(view, "\n") {
			if pos := strings.Index(line, "Allow (a)"); pos >= 0 {
				x := ansi.StringWidth(line[:pos])
				_, cmd = m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				require.NotNil(t, cmd)
				response := cmd().(PermissionResponseMsg)
				require.Equal(t, PermissionAllow, response.Action)
				require.Equal(t, request, response.Permission)
				clicked = true
			}
		}
		require.True(t, clicked)
	}
}

func TestDisabledProviderVisibleAndEnableRequiresExplicitSave(t *testing.T) {
	m := settingsDialog(t)
	config.Get().ProviderProfiles["studio"] = config.ProviderProfile{Name: "Studio", Provider: config.Provider{Disabled: true}}
	m.Open()
	require.Contains(t, ansi.Strip(m.View()), "disabled")
	m.edit(&providerRow{id: "studio"})
	require.Contains(t, m.fieldIndices(), 8)
	require.Equal(t, "false", m.fields[8].Value())
	_, profile, err := m.readProfile()
	require.NoError(t, err)
	require.True(t, profile.Disabled)
	m.focus = 8
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	_, profile, err = m.readProfile()
	require.NoError(t, err)
	require.False(t, profile.Disabled)
	require.True(t, config.Get().ProviderProfiles["studio"].Disabled, "editing must not persist or activate")
}

func TestInitializeFitsCompactTerminal(t *testing.T) {
	settingsDialog(t)
	for _, lang := range []string{"en", "pt"} {
		config.Get().TUI.Language = lang
		m := InitDialogCmp{}
		m.SetSize(60, 20)
		view := m.View()
		require.LessOrEqual(t, lipgloss.Width(view), 60)
		require.LessOrEqual(t, lipgloss.Height(view), 20)
		require.Contains(t, ansi.Strip(view), config.Text("Agora não", "Not now"))
	}
}

func TestProviderBasicMouseFocusMatchesVisibleInput(t *testing.T) {
	m := settingsDialog(t)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m.edit(nil)
	for _, tc := range []struct {
		label string
		field int
	}{{"Name", 1}, {"API base URL", 3}, {"API key environment variable", 4}, {"Connection active", 8}} {
		found := false
		for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			if strings.Contains(line, tc.label) {
				m.Update(tea.MouseMsg{X: 5, Y: y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				require.Equal(t, tc.field, m.focus)
				require.True(t, m.fields[tc.field].Focused(), tc.label)
				if tc.field != 8 {
					m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
					m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("typed")})
					require.Contains(t, m.fields[tc.field].Value(), "typed")
				}
				found = true
				break
			}
		}
		require.True(t, found, tc.label)
	}
}
