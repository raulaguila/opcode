package dialog

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

func (m *providerDialogCmp) providerBodyLines(t theme.Theme, width int, base lipgloss.Style, line func(string) string) []string {
	if m.screen != "list" {
		return m.providerFormLines(width, line)
	}
	var lines []string
	if len(m.rows) == 0 {
		lines = append(lines, line("No connections yet. Press N to add Ollama or another provider."))
	}
	rows := m.listRows()
	start := max(0, m.selected-rows+1)
	for i := start; i < min(len(m.rows), start+rows); i++ {
		style := base
		if i == m.selected {
			style = style.Foreground(t.Text()).Background(t.BorderDim()).Bold(true)
		}
		lines = append(lines, style.Render(ansi.Truncate(selectionMark(i == m.selected)+m.providerRowLabel(m.rows[i]), width, "…")))
	}
	lines = append(lines, styles.PanelDivider(width))
	primary, secondary := m.providerListActions()
	for _, action := range primary {
		lines = append(lines, line(action))
	}
	for _, action := range secondary {
		lines = append(lines, dialogHint(action, width))
	}
	return lines
}

func (m *providerDialogCmp) providerListActions() (primary, secondary []string) {
	primary = wrapDialogActions(config.Text("[Adicionar] N · [Editar] Enter · [Excluir] D · Esc", "[Add] N · [Edit] Enter · [Delete] D · Esc"), m.contentWidth())
	secondary = wrapDialogActions(config.Text("[Preferências] P · [Integrações] I", "[Preferences] P · [Integrations] I"), m.contentWidth())
	return primary, secondary
}

func wrapDialogActions(actions string, width int) []string {
	var rows []string
	for _, action := range strings.Split(actions, " · ") {
		last := len(rows) - 1
		if last < 0 || ansi.StringWidth(rows[last]+" · "+action) > width {
			rows = append(rows, ansi.Truncate(action, width, "…"))
		} else {
			rows[last] += " · " + action
		}
	}
	return rows
}

func (m *providerDialogCmp) providerRowLabel(row providerRow) string {
	var label string
	var disabled bool
	if row.legacy {
		label = config.Text("Servidor local", "Local server") + " · " + row.id
		disabled = config.Get().Providers[models.ModelProvider(row.id)].Disabled
	} else {
		profile := config.Get().ProviderProfiles[row.id]
		label = profile.Name + " · " + row.id + " (" + string(profile.Backend) + ")"
		disabled = profile.Disabled
	}
	if disabled {
		label += config.Text(" · desativada", " · disabled")
	} else {
		label += config.Text(" · configurada", " · configured")
	}
	return label
}

func (m *providerDialogCmp) providerFormLines(width int, line func(string) string) []string {
	m.configureProfilePlaceholders()
	count, indices := m.formRows(), m.fieldIndices()
	start := max(0, slices.Index(indices, m.focus)-count+1)
	var lines []string
	for _, i := range indices[start:min(len(indices), start+count)] {
		label := fieldLabel(m.labels[i])
		if i == m.focus {
			label = "› " + label
		}
		lines = append(lines, line(label))
		m.fields[i].SetWidth(max(8, width-2))
		lines = append(lines, line(choiceView(m.fields[i], fieldOptions(m.labels[i]))))
	}
	hint := line(m.providerFormHint())
	if m.screen != "provider" {
		hint = dialogHint(m.providerFormHint(), width)
	}
	return append(lines, styles.PanelDivider(width), hint)
}

func (m *providerDialogCmp) providerFormHint() string {
	if m.screen != "provider" {
		return "Tab/↑↓ fields · Ctrl+S save · Esc back"
	}
	hint := config.Text("[Salvar] [Catálogo] [Mais] · Esc", "[Save] [Catalog] [More] · Esc")
	if m.fields[providerActiveField].Value() == "false" {
		return config.Text("Ctrl+G salvar e ativar · ", "Ctrl+G save and enable · ") + hint
	}
	return hint
}
