package dialog

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestMenusHandleEmptyListsAndOutsideClicks(t *testing.T) {
	settingsDialog(t)
	f := &filepickerCmp{cwd: newDialogInput(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(22)), cwdDetails: &DirNode{directory: t.TempDir()}}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Text: "l"}} {
		require.NotPanics(t, func() { f.Update(key) })
	}
	m := NewProviderDialogCmp().(*providerDialogCmp)
	m.screen = "presets"
	m.width = 80
	m.height = 24
	for _, point := range [][2]int{{5, 4 + len(config.ProviderPresets)}, {-1, 4}, {5, 100}} {
		require.NotPanics(t, func() {
			m.Update(tea.MouseClickMsg{X: point[0], Y: point[1], Button: tea.MouseLeft})
		})
		require.Equal(t, "presets", m.screen)
	}
}
func TestSecondaryMenusFitAndPreserveFocus(t *testing.T) {
	settingsDialog(t)
	for _, size := range []tea.WindowSizeMsg{{Width: 60, Height: 20}, {Width: 80, Height: 24}, {Width: 120, Height: 36}} {
		f := &filepickerCmp{cwd: newDialogInput(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(22)), cwdDetails: &DirNode{directory: t.TempDir()}}
		f.Update(size)
		require.LessOrEqual(t, lipgloss.Width(f.View()), size.Width)
		require.LessOrEqual(t, lipgloss.Height(f.View()), size.Height)
		m := NewMultiArgumentsDialogCmp("demo", "", []string{"first", "second", "third", "fourth", "fifth"})
		m.SetSize(size.Width, size.Height)
		require.LessOrEqual(t, lipgloss.Width(m.View()), size.Width)
		require.LessOrEqual(t, lipgloss.Height(m.View()), size.Height)
		u, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = u.(MultiArgumentsDialogCmp)
		u, _ = m.Update(size)
		m = u.(MultiArgumentsDialogCmp)
		m.Init()
		u, _ = m.Update(tea.KeyPressMsg{Text: "ação"})
		m = u.(MultiArgumentsDialogCmp)
		require.Equal(t, "ação", m.inputs[1].Value())
		require.Empty(t, m.inputs[0].Value())
	}
}
func TestSessionAndThemeSelectionRemainVisible(t *testing.T) {
	settingsDialog(t)
	m := NewSessionDialogCmp().(*sessionDialogCmp)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	var ss []session.Session
	for i := range 10 {
		ss = append(ss, session.Session{ID: strings.Repeat("x", i+1), Title: strings.Repeat("x", i+1)})
	}
	m.SetSessions(ss)
	for range 9 {
		m.move(1)
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	require.Contains(t, ansi.Strip(m.View()), "›")
	before := theme.ColorProfile()
	theme.SetColorProfile(colorprofile.Ascii)
	defer theme.SetColorProfile(before)
	theme := NewThemeDialogCmp().(*themeDialogCmp)
	theme.Init()
	v := theme.View()
	theme.selectedIdx = (theme.selectedIdx + 1) % len(theme.themes)
	require.NotEqual(t, v, theme.View())
	_, cmd := NewQuitCmp().Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.NotNil(t, cmd)
	require.IsType(t, CloseQuitMsg{}, cmd())
}
func TestIntegrationFieldsFollowTransportWithoutLosingValues(t *testing.T) {
	settingsDialog(t)
	m := NewIntegrationDialogCmp(nil).(*integrationDialogCmp)
	m.edit(config.Integration{Kind: "mcp", MCP: config.MCPServer{Type: config.MCPStdio}})
	m.fields[4].SetValue("https://example.invalid/mcp")
	require.Contains(t, m.visibleFields(), 2)
	require.NotContains(t, m.visibleFields(), 4)
	m.fields[1].SetValue("http")
	require.Contains(t, m.visibleFields(), 4)
	require.NotContains(t, m.visibleFields(), 2)
	require.NotContains(t, m.visibleFields(), 9)
	m.fields[8].SetValue("true")
	require.Contains(t, m.visibleFields(), 9)
	require.Equal(t, "https://example.invalid/mcp", m.fields[4].Value())
}

func TestFormDraftAndCompleteErrorArePreserved(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[1].SetValue("My edited provider")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.Equal(t, "list", m.screen)
	m.Update(tea.KeyPressMsg{Text: "r"})
	require.Equal(t, "My edited provider", m.fields[1].Value())
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m.note = strings.Repeat("long error ", 100)
	require.LessOrEqual(t, lipgloss.Height(m.View()), 20)
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	require.True(t, m.noteFull)
	require.LessOrEqual(t, lipgloss.Height(m.View()), 20)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.Equal(t, "My edited provider", m.fields[1].Value())
}
func TestPairEditorRejectsDuplicateNamesAndKeepsLiteralReferences(t *testing.T) {
	a, err := newPairList(`{"Authorization":"TOKEN_ENV"}`)
	require.NoError(t, err)
	a.add("X-Custom=OTHER_ENV")
	done, save, _ := a.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	require.True(t, done)
	require.True(t, save)
	require.JSONEq(t, `{"Authorization":"TOKEN_ENV","X-Custom":"OTHER_ENV"}`, a.value())
	a.add("Authorization=DUP_ENV")
	done, save, _ = a.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	require.False(t, done)
	require.False(t, save)
}
func TestModeChooserDoesNotHaveInvisibleSearch(t *testing.T) {
	settingsDialog(t)
	m := NewCommandDialogCmp().(*commandDialogCmp)
	m.SetCommands([]Command{{ID: "mode-ask", Title: "Ask"}, {ID: "mode-plan", Title: "Plan"}, {ID: "mode-execute", Title: "Execute"}})
	m.SetContext("Work mode", "mode-ask")
	m.Update(tea.KeyPressMsg{Text: "zzzz"})
	require.Len(t, m.listView.GetItems(), 3)
	require.Empty(t, m.search.Value())
}

func TestDetailsDoNotSwallowProbeCompletion(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.testing = true
	m.note = "working"
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	require.True(t, m.noteFull)
	request := m.request
	m.Update(providerTestMsg{request: request, count: 3})
	require.False(t, m.testing)
	m.Close()
	require.False(t, m.noteFull)
}

func TestResumedDraftRejectsCancelledProbeResult(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	request := m.request
	m.fields[1].SetValue("draft")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(tea.KeyPressMsg{Text: "r"})
	require.Greater(t, m.request, request)
	m.note = "preserved"
	m.Update(providerTestMsg{request: request, count: 8})
	require.Equal(t, "preserved", m.note)
	require.Equal(t, "draft", m.fields[1].Value())
}
