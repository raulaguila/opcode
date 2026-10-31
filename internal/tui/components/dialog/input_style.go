package dialog

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

func dialogBody() lipgloss.Style { return styles.Panel().UnsetBorderStyle() }

func dialogHint(text string, width int) string {
	return styles.PanelHint().Width(max(1, width)).Render(ansi.Truncate(interfaceCopy(text), max(1, width), "…"))
}

func dialogFooter(rows []string, width, height int, controls ...string) []string {
	if height == 0 || height >= 16 {
		rows = append(rows, styles.PanelDivider(width))
	}
	return append(rows, controls...)
}

func newDialogInput() textinput.Model {
	input := textinput.New()
	input.SetVirtualCursor(true)
	t := theme.CurrentTheme()
	base := dialogBody().Background(t.BackgroundSecondary())
	state := textinput.StyleState{Text: base, Prompt: base, Placeholder: base.Foreground(t.TextMuted()), Suggestion: base.Foreground(t.TextMuted())}
	s := input.Styles()
	s.Focused, s.Blurred = state, state
	s.Focused.Prompt = base.Foreground(t.Primary())
	s.Cursor.Color = t.Text()
	input.SetStyles(s)
	return input
}

func leftClick(msg tea.MouseMsg) bool {
	_, clicked := msg.(tea.MouseClickMsg)
	return clicked && msg.Mouse().Button == tea.MouseLeft
}
