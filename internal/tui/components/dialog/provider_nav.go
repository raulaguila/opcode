package dialog

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
)

const settingsNavWidth = 16

// settingsNavWidth is the section column on wide terminals; 0 hides it.
func (m *providerDialogCmp) navWidth() int {
	switch m.screen {
	case "list", "settings", "effective":
		if m.width >= 110 {
			return settingsNavWidth
		}
	}
	return 0
}

// navOffset is how far the nav column shifts the content to the right.
func (m *providerDialogCmp) navOffset() int {
	if w := m.navWidth(); w > 0 {
		return w + 3
	}
	return 0
}

// settingsSections are the nav rows: connections, the preference categories
// and integrations (its own dialog).
var settingsCategories = []string{"", "interface", "execution", "shortcuts"}

func settingsSections() []string {
	return []string{config.Text("Conexões", "Connections"), config.Text("Interface", "Interface"), config.Text("Trabalho", "Work"), config.Text("Atalhos", "Shortcuts"), config.Text("Integrações", "Integrations")}
}

func (m *providerDialogCmp) navRows() []string {
	current := 0
	if m.screen == "settings" || m.screen == "effective" {
		current = max(1, slices.Index(settingsCategories, m.category))
	}
	var rows []string
	for i, section := range settingsSections() {
		text := ansi.Truncate(selectionMark(i == current)+section, settingsNavWidth, "…")
		text += strings.Repeat(" ", max(0, settingsNavWidth-ansi.StringWidth(text)))
		if i == current {
			rows = append(rows, styles.SelectedRow().Bold(true).Render(text))
		} else {
			rows = append(rows, dialogBody().Render(text))
		}
	}
	return rows
}

// withNav places the section column left of the dialog body.
func (m *providerDialogCmp) withNav(lines []string) []string {
	if m.navWidth() == 0 {
		return lines
	}
	return besideColumns(m.navRows(), lines, settingsNavWidth)
}

// navClick switches section from a click on the nav column, reusing the
// same keys the legend advertises (P preferences, I integrations, Esc back).
func (m *providerDialogCmp) navClick(msg tea.Msg) (tea.Cmd, bool) {
	mouse, ok := msg.(tea.MouseMsg)
	if !ok || m.navWidth() == 0 || !leftClick(mouse) || mouse.Mouse().X < 3 || mouse.Mouse().X >= 3+settingsNavWidth {
		return nil, false
	}
	row := mouse.Mouse().Y - 2
	if row < 0 || row >= len(settingsSections()) {
		return nil, true
	}
	switch {
	case row >= 1 && row < len(settingsCategories):
		var cmd tea.Cmd
		if m.screen == "list" {
			_, cmd = m.Update(tea.KeyPressMsg{Text: "p"})
		}
		m.category, m.selected = settingsCategories[row], 0
		return cmd, true
	case m.screen != "list":
		m.category = ""
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	}
	if row == len(settingsCategories) {
		_, cmd := m.Update(tea.KeyPressMsg{Text: "i"})
		return cmd, true
	}
	return nil, true
}
