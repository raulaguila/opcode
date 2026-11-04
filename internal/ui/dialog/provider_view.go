package dialog

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/ui/styles"
	"github.com/raulaguila/opcode/internal/ui/theme"
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
	// Each connection is a two-line card: status and name, then its address
	// and authentication.
	for i := start; i < min(len(m.rows), start+rows); i++ {
		style, detail := base, styles.PanelHint().Width(width)
		if i == m.selected {
			style = style.Foreground(t.Text()).Background(t.BorderDim()).Bold(true)
			detail = detail.Background(t.BorderDim())
		}
		dot, enabled := "● ", !m.providerDisabled(m.rows[i])
		if !enabled {
			dot = "○ "
		}
		lines = append(lines,
			style.Render(ansi.Truncate(selectionMark(i == m.selected)+dot+m.providerRowLabel(m.rows[i]), width, "…")),
			detail.Render(ansi.Truncate("    "+m.providerRowDetail(m.rows[i]), width, "…")))
	}
	if m.navWidth() > 0 {
		// Wide: sections are in the nav and actions in the frame's legend.
		return append(lines, styles.PanelHint().Render(config.Text("  + Nova conexão · N · Ollama, OpenAI, Anthropic, Gemini, OpenRouter…", "  + New connection · N · Ollama, OpenAI, Anthropic, Gemini, OpenRouter…")))
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

func (m *providerDialogCmp) providerDisabled(row providerRow) bool {
	if row.legacy {
		return config.Get().Providers[models.ModelProvider(row.id)].Disabled
	}
	return config.Get().ProviderProfiles[row.id].Disabled
}

// providerRowDetail is a card's second line: address and authentication.
func (m *providerDialogCmp) providerRowDetail(row providerRow) string {
	provider := config.Get().Providers[models.ModelProvider(row.id)]
	if !row.legacy {
		provider = config.Get().ProviderProfiles[row.id].Provider
	}
	address := provider.BaseURL
	if address == "" {
		address = config.Text("endereço padrão do provedor", "provider default address")
	}
	auth := config.Text("sem autenticação", "no authentication")
	switch {
	case provider.APIKeyRef != "":
		auth = config.Text("chave no cofre do sistema", "key in system vault")
	case provider.APIKeyEnv != "":
		auth = config.Text("chave em ", "key in ") + provider.APIKeyEnv
	case provider.APIKey != "":
		auth = config.Text("chave no arquivo de configuração", "key in config file")
	}
	return address + " · " + auth
}

func (m *providerDialogCmp) providerFormLines(width int, line func(string) string) []string {
	m.configureProfilePlaceholders()
	count, indices := m.formRows(), m.fieldIndices()
	start := max(0, slices.Index(indices, m.focus)-count+1)
	var lines []string
	// One row per field: the label in a left column, the value beside it.
	labelWidth := 10
	for _, i := range indices[start:min(len(indices), start+count)] {
		labelWidth = max(labelWidth, ansi.StringWidth(fieldLabel(m.labels[i]))+2)
	}
	labelWidth = min(labelWidth, width/2)
	valueWidth := max(8, width-labelWidth-2)
	for _, i := range indices[start:min(len(indices), start+count)] {
		name := ansi.Truncate(fieldLabel(m.labels[i]), labelWidth-2, "…")
		pad := strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(name)-2))
		label := styles.PanelHint().Render("  " + name + pad)
		if i == m.focus {
			label = styles.PanelTitle().Render("› "+name) + styles.PanelHint().Render(pad)
		}
		m.fields[i].SetWidth(max(8, valueWidth-1))
		lines = append(lines, line(label+dialogBody().Render("  ")+choiceRadios(m.fields[i], fieldOptions(m.labels[i]), valueWidth)))
	}
	if m.screen == "provider" {
		// The advanced toggle, then the actions as buttons.
		advanced := config.Text("› Avançado · Ctrl+A: janela de contexto, tokens de saída, TLS", "› Advanced · Ctrl+A: context window, output tokens, TLS")
		if m.advanced {
			advanced = config.Text("‹ Ocultar avançado · Ctrl+A", "‹ Hide advanced · Ctrl+A")
		}
		lines = append(lines, "", dialogHint(advanced, width), "")
		t := theme.CurrentTheme()
		chip := styles.BaseStyle().Background(t.BorderDim()).Foreground(styles.ReadableForeground(t.Text(), t.BorderDim()))
		primary := styles.BaseStyle().Background(t.Primary()).Foreground(styles.ReadableForeground(t.Background(), t.Primary())).Bold(true)
		for _, row := range wrapDialogActions(m.providerFormHint(), width) {
			var parts []string
			for i, action := range strings.Split(row, " · ") {
				style := chip
				if i == 0 && strings.Contains(action, "Ctrl+S") || strings.Contains(action, "Ctrl+G") {
					style = primary
				}
				parts = append(parts, style.Render(" "+strings.ReplaceAll(action, " — ", " · ")+" "))
			}
			lines = append(lines, strings.Join(parts, dialogBody().Render("  ")))
		}
		return lines
	}
	lines = append(lines, styles.PanelDivider(width))
	for _, hint := range wrapDialogActions(m.providerFormHint(), width) {
		lines = append(lines, dialogHint(hint, width))
	}
	return lines
}

func (m *providerDialogCmp) providerFormHint() string {
	if m.screen != "provider" {
		return "Tab/↑↓ fields · Ctrl+S save · Esc back"
	}
	// Buttons, separated by " · "; " — " joins a label to its key.
	hint := config.Text("Salvar — Ctrl+S · Testar catálogo — Ctrl+T · Cancelar — Esc", "Save — Ctrl+S · Test catalog — Ctrl+T · Cancel — Esc")
	if m.fields[providerActiveField].Value() == "false" {
		return config.Text("Salvar e ativar — Ctrl+G · ", "Save and enable — Ctrl+G · ") + hint
	}
	return hint
}
