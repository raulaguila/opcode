package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestApprovalResetsConsentAndSupportsEscapeAndExactMouseTargets(t *testing.T) {
	settingsDialog(t)
	for _, width := range []int{60, 80, 120} {
		m := NewPermissionDialogCmp().(*permissionDialogCmp)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		request := permission.PermissionRequest{ID: "first", ToolName: "test", Path: "/project", Params: map[string]string{"action": "exact"}}
		m.SetPermissions(request)
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		require.Equal(t, PermissionDeny, cmd().(PermissionResponseMsg).Action)
		m.selectedOption = 1
		request.ID = "second"
		m.SetPermissions(request)
		require.Equal(t, 2, m.selectedOption)
		_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		require.Equal(t, PermissionDeny, cmd().(PermissionResponseMsg).Action)
		// Use the actual rendered label, not the component's hit rectangles.
		view := ansi.Strip(m.View())
		clicked := false
		for y, line := range strings.Split(view, "\n") {
			if before, _, ok := strings.Cut(line, "Allow (a)"); ok {
				x := ansi.StringWidth(before)
				_, cmd = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
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

func TestNativeSpaceConfirmsOnlyTheSelectedConsentChoice(t *testing.T) {
	settingsDialog(t)
	space := tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	quit := NewQuitCmp()
	_, cmd := quit.Update(space)
	require.NotNil(t, cmd)
	require.IsType(t, CloseQuitMsg{}, cmd(), "space keeps Continue as the default")
	quit.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	_, cmd = quit.Update(space)
	require.IsType(t, ConfirmQuitMsg{}, cmd())

	approval := NewPermissionDialogCmp()
	request := permission.PermissionRequest{ID: "space", ToolName: "test", Path: "/project"}
	approval.SetPermissions(request)
	_, cmd = approval.Update(space)
	require.NotNil(t, cmd)
	require.Equal(t, PermissionDeny, cmd().(PermissionResponseMsg).Action)
	approval.SetPermissions(request)
	approval.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	approval.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	_, cmd = approval.Update(space)
	response := cmd().(PermissionResponseMsg)
	require.Equal(t, PermissionAllow, response.Action)
	require.Equal(t, request, response.Permission)
}

func TestNativeKeyTextDoesNotDiscardConsentShortcutModifiers(t *testing.T) {
	settingsDialog(t)
	for _, mod := range []tea.KeyMod{tea.ModAlt, tea.ModCtrl, tea.ModShift, tea.ModMeta} {
		approval := NewPermissionDialogCmp().(*permissionDialogCmp)
		approval.SetPermissions(permission.PermissionRequest{ID: "modifiers", ToolName: "test"})
		for _, code := range []rune{'a', 's', 'd', tea.KeyEnter, tea.KeySpace} {
			_, cmd := approval.Update(tea.KeyPressMsg{Code: code, Text: string(code), Mod: mod})
			require.Nil(t, cmd, "modified key must not approve or answer a permission: %v/%v", mod, code)
			require.Equal(t, 2, approval.selectedOption)
		}
	}
	quit := NewQuitCmp()
	for _, mod := range []tea.KeyMod{tea.ModAlt, tea.ModCtrl, tea.ModMeta} {
		_, cmd := quit.Update(tea.KeyPressMsg{Code: 'y', Text: "y", Mod: mod})
		require.Nil(t, cmd, "modified Y must not quit")
		_, cmd = NewInitDialogCmp().Update(tea.KeyPressMsg{Code: 'y', Text: "y", Mod: mod})
		require.Nil(t, cmd, "modified Y must not initialize a project")
	}
	_, cmd := NewInitDialogCmp().Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	require.True(t, cmd().(CloseInitDialogMsg).Initialize, "plain Y still initializes explicitly")
}

func TestDisabledProviderVisibleAndEnableRequiresExplicitSave(t *testing.T) {
	m := settingsDialog(t)
	config.Get().ProviderProfiles["studio"] = config.ProviderProfile{Name: "Studio", Disabled: true}
	m.Open()
	require.Contains(t, ansi.Strip(m.View()), "disabled")
	m.edit(&providerRow{id: "studio"})
	require.Contains(t, m.fieldIndices(), 8)
	require.Equal(t, "false", m.fields[8].Value())
	_, profile, err := m.readProfile()
	require.NoError(t, err)
	require.True(t, profile.Disabled)
	m.focus = 8
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
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
	m.fields[providerAuthField].SetValue("environment")
	for _, tc := range []struct {
		label string
		field int
	}{{"Name", 1}, {"API base URL", 3}, {"API key environment variable", 4}, {"Connection active", 8}} {
		found := false
		for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			if strings.Contains(line, tc.label) {
				m.Update(tea.MouseClickMsg{X: 5, Y: y + 1, Button: tea.MouseLeft})
				require.Equal(t, tc.field, m.focus)
				require.True(t, m.fields[tc.field].Focused(), tc.label)
				if tc.field != 8 {
					m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
					m.Update(tea.KeyPressMsg{Text: "typed"})
					require.Contains(t, m.fields[tc.field].Value(), "typed")
				}
				found = true
				break
			}
		}
		require.True(t, found, tc.label)
	}
}
