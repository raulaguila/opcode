package dialog

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

func (m *integrationDialogCmp) integrationBodyLines(t theme.Theme, width int, base lipgloss.Style, line func(string) string) []string {
	switch m.screen {
	case "list":
		return m.integrationListLines(t, width, base, line)
	case "edit":
		return m.integrationFormLines(width, base, line)
	default:
		rows := max(1, m.height-8)
		if m.height == 0 {
			rows = 12
		}
		var lines []string
		for i := m.resultOffset; i < min(len(m.result), m.resultOffset+rows); i++ {
			lines = append(lines, line(m.result[i]))
		}
		return lines
	}
}

func (m *integrationDialogCmp) integrationListLines(t theme.Theme, width int, base lipgloss.Style, line func(string) string) []string {
	var lines []string
	rows := m.listRows()
	start := max(0, m.selected-rows+1)
	if len(m.entries) == 0 {
		lines = append(lines, line("No integrations. N adds MCP; L adds a language server."))
	}
	for i := start; i < min(len(m.entries), start+rows); i++ {
		style := base
		if i == m.selected {
			style = style.Foreground(t.Text()).Background(t.BorderDim()).Bold(true)
		}
		lines = append(lines, style.Render(ansi.Truncate(selectionMark(i == m.selected)+config.IntegrationSummary(m.entries[i]), width, "…")))
	}
	return lines
}

func (m *integrationDialogCmp) integrationFieldWindow() ([]int, int, int) {
	indices := m.visibleFields()
	count := min(m.formRows(), len(indices))
	return indices, count, max(0, slices.Index(indices, m.focus)-count+1)
}

func (m *integrationDialogCmp) integrationFormLines(width int, base lipgloss.Style, line func(string) string) []string {
	indices, count, start := m.integrationFieldWindow()
	var lines []string
	for _, i := range indices[start:min(len(indices), start+count)] {
		label := fieldLabel(m.labels[i])
		if strings.HasPrefix(m.labels[i], "Arguments") {
			label = config.Text("Argumentos · Enter edita lista", "Arguments · Enter edits list")
		}
		if i == m.focus {
			label = "› " + label
		}
		lines = append(lines, line(label))
		m.fields[i].SetWidth(max(8, width-2))
		lines = append(lines, base.Render(ansi.Truncate(choiceView(m.fields[i], fieldOptions(m.labels[i])), width, "…")))
	}
	return lines
}
