package dialog

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/ui/styles"
	"github.com/raulaguila/opcode/internal/ui/theme"
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
	// Connections and integrations show how many are configured.
	counts := map[int]int{0: len(m.rows)}
	if cfg := config.Get(); cfg != nil {
		counts[len(settingsCategories)] = len(cfg.LSP) + len(cfg.MCPServers)
	}
	var rows []string
	for i, section := range settingsSections() {
		mark := "  "
		if i == current {
			mark = "▌ "
		}
		count := ""
		if n, ok := counts[i]; ok && n > 0 {
			count = fmt.Sprint(n)
		}
		text := ansi.Truncate(mark+section, settingsNavWidth-ansi.StringWidth(count)-1, "…")
		text += strings.Repeat(" ", max(1, settingsNavWidth-ansi.StringWidth(text)-ansi.StringWidth(count))) + count
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

// besideColumns draws two columns of rows split by a rule.
func besideColumns(left, right []string, leftWidth int) []string {
	fill := dialogBody()
	rule := dialogBody().Foreground(theme.CurrentTheme().BorderNormal()).Render(" │ ")
	rows := make([]string, 0, max(len(left), len(right)))
	for i := range max(len(left), len(right)) {
		l, r := "", ""
		if i < len(left) {
			l = ansi.Truncate(left[i], leftWidth, "…")
		}
		if i < len(right) {
			r = right[i]
		}
		gap := fill.Render(strings.Repeat(" ", max(0, leftWidth-ansi.StringWidth(l))))
		rows = append(rows, l+gap+rule+r)
	}
	return rows
}
