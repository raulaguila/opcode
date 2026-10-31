package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func legendGlyph(t *testing.T, view, text string) vt10x.Glyph {
	t.Helper()
	screen := vt10x.New(vt10x.WithSize(180, 80))
	_, err := screen.Write([]byte(strings.ReplaceAll(styles.TerminalOutput(view), "\n", "\r\n")))
	require.NoError(t, err)
	for y, row := range strings.Split(ansi.Strip(view), "\n") {
		if before, _, found := strings.Cut(row, text); found {
			return screen.Cell(ansi.StringWidth(before), y)
		}
	}
	t.Fatalf("missing rendered text %q in %s", text, ansi.Strip(view))
	return vt10x.Glyph{}
}

func TestModelPickerUsageMarkerBelongsOnlyToActiveRow(t *testing.T) {
	m := populatedModelDialog(t)
	for _, language := range []string{"en", "pt"} {
		config.Get().TUI.Language = language
		for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 12}, {Width: 100, Height: 28}} {
			m.Update(size)
			m.selectedIdx = 1
			m.keepVisible()
			view := ansi.Strip(m.View())
			require.Contains(t, view, config.Text("Em uso", "In use"))
			require.Equal(t, 1, strings.Count(view, "✓"), "only the active model row carries a check")
			require.NotContains(t, view, "✓ current")
			require.NotContains(t, view, "✓ atual")
			require.Contains(t, view, config.Text("2 de 16", "2 of 16"))
			require.LessOrEqual(t, lipgloss.Width(m.View()), size.Width)
			require.LessOrEqual(t, lipgloss.Height(m.View()), size.Height)
			rows := strings.Split(view, "\n")
			require.Contains(t, rows[m.geometry().listY], "✓")
			require.Contains(t, rows[m.geometry().listY+1], "›")
		}
	}
	m.selectedIdx = len(m.filtered) - 1
	m.keepVisible()
	require.NotContains(t, ansi.Strip(m.View()), "✓", "offscreen active rows do not add a usage legend to the footer")
	config.Get().Agents[config.AgentCoder] = config.Agent{Model: "not-in-this-catalog"}
	view := ansi.Strip(m.View())
	require.NotContains(t, view, "✓")
	require.NotContains(t, view, "Em uso")
}

func TestMenuLegendsAreSecondaryWhileSelectionAndConsentStayDistinct(t *testing.T) {
	m := populatedModelDialog(t)
	profile, name := theme.ColorProfile(), theme.CurrentThemeName()
	t.Cleanup(func() { theme.SetColorProfile(profile); _ = theme.PreviewTheme(name) })
	require.NoError(t, theme.PreviewTheme("opcode"))
	theme.SetColorProfile(colorprofile.TrueColor)
	m.selectedIdx = 1
	hint := legendGlyph(t, styles.PanelHint().Render("legend"), "legend")
	body := legendGlyph(t, dialogBody().Render("body"), "body")
	view := m.View()
	actual := legendGlyph(t, view, "Enter choose")
	require.Equal(t, hint.FG, actual.FG)
	require.NotEqual(t, body.FG, actual.FG)
	require.Zero(t, actual.Mode&23)
	require.NotZero(t, legendGlyph(t, view, "›   Model 1").Mode&4, "navigation highlight remains bold")
	p := NewPermissionDialogCmp().(*permissionDialogCmp)
	p.permission.ToolName = tools.BashToolName
	consent := legendGlyph(t, p.permissionHintView(100), "exact scope")
	require.Equal(t, body.FG, consent.FG, "consent scope must retain normal text emphasis")
	p.permission.ToolName = tools.EditToolName
	require.Equal(t, hint.FG, legendGlyph(t, p.permissionHintView(100), "T diff").FG)
}

func TestIntegrationErrorNotesAreNotStyledAsLegends(t *testing.T) {
	settingsDialog(t)
	profile := theme.ColorProfile()
	t.Cleanup(func() { theme.SetColorProfile(profile) })
	theme.SetColorProfile(colorprofile.TrueColor)
	m := NewIntegrationDialogCmp(nil).(*integrationDialogCmp)
	m.screen, m.note = "result", "Test failed: fixture diagnostic"
	view := styles.Panel().Render(strings.Join(m.integrationNoteLines(m.note), "\n"))
	normal := legendGlyph(t, dialogBody().Render("body"), "body")
	require.Equal(t, normal.FG, legendGlyph(t, view, "Test failed").FG)
	require.Equal(t, "Test failed: fixture diagnostic", m.note, "styling never changes stored diagnostics")
}
