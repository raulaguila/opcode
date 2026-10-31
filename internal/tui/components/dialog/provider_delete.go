package dialog

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (m *providerDialogCmp) startDelete() tea.Cmd {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return nil
	}
	m.Close()
	m.deleting = m.rows[m.selected]
	m.deleteSelected = false
	m.screen, m.note = "delete-provider", ""
	return nil
}

func (m *providerDialogCmp) deleteUpdate(msg tea.Msg) (tea.Cmd, bool) {
	if m.screen != "delete-provider" {
		return nil, false
	}
	if m.noteUpdate(msg) {
		return nil, true
	}
	confirm := false
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			return m.Open(), true
		case "left", "right", "tab", "shift+tab":
			m.deleteSelected = !m.deleteSelected
		case "enter":
			if !m.deleteSelected {
				return m.Open(), true
			}
			confirm = true
		}
	case tea.MouseMsg:
		if clickedLabel(m.deleteView(), config.Text("[Cancelar]", "[Cancel]"), msg) {
			return m.Open(), true
		}
		confirm = clickedLabel(m.deleteView(), config.Text("[Excluir]", "[Delete]"), msg)
	}
	if result0, result1, handled := deleteUpdateConfirm(m, confirm); handled {
		return result0, result1
	}
	return nil, true
}

func (m *providerDialogCmp) deleteView() string {
	t := theme.CurrentTheme()
	base := dialogBody().Width(m.contentWidth())
	line := func(s string) string { return base.Render(ansi.Truncate(s, m.contentWidth(), "…")) }
	label := m.deleting.id
	if !m.deleting.legacy {
		if p, ok := config.Get().ProviderProfiles[m.deleting.id]; ok && p.Name != "" {
			label = p.Name + " · " + label
		}
	}
	cancel := dialogBody().Render(config.Text("[Cancelar]", "[Cancel]"))
	remove := dialogBody().Render(config.Text("[Excluir]", "[Delete]"))
	selected := dialogBody().Background(t.BorderDim()).Foreground(t.Text()).Bold(true)
	if m.deleteSelected {
		remove = selected.Render(config.Text("[Excluir]", "[Delete]"))
	} else {
		cancel = selected.Render(config.Text("[Cancelar]", "[Cancel]"))
	}
	label = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, label)
	lines := []string{
		line(config.Text("Excluir conexão?", "Delete connection?")), line(label),
		line(config.Text("Remove credenciais e modelos salvos.", "Removes saved credentials and models.")),
		line(config.Text("Troque modelos em uso antes de excluir.", "Replace models in use before deleting.")),
		base.Render(cancel + "  " + remove),
	}
	lines = append(lines, m.noteLines(m.note)...)
	lines = dialogFooter(lines, m.contentWidth(), m.height, dialogHint(config.Text("←/→ escolhe · Enter · Esc", "←/→ choose · Enter · Esc"), m.contentWidth()))
	return styles.Panel().Padding(1, 2).Border(lipgloss.NormalBorder()).BorderForeground(t.TextMuted()).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func deleteUpdateConfirm(m *providerDialogCmp, confirm bool) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	if confirm {
		if m.allowSave != nil {
			if err := m.allowSave(); err != nil {
				m.note = err.Error()
				return nil, true, true
			}
		}
		if err := config.DeleteProvider(m.deleting.id, m.deleting.legacy); err != nil {
			m.note = err.Error()
			return nil, true, true
		}
		m.Open()
		return util.CmdHandler(ProviderSettingsSavedMsg{ProviderChanged: true, Deleted: true}), true, true
	}
	return extractedResult0, extractedResult1, false
}
