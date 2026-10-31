package dialog

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type ReasoningSelectedMsg struct {
	Agent  config.AgentName
	Model  models.ModelID
	Effort string
}

func (m *modelDialogCmp) reasoningModel() models.Model {
	role := m.target
	if role == "" {
		role = config.AgentCoder
	}
	model := config.ModelWithProviderDefaults(models.SupportedModels[config.Get().Agents[role].Model])
	if p, err := config.ProviderForModel(model); err == nil && p.API == "ollama" {
		model.API = "ollama"
	}
	return model
}
func (m *modelDialogCmp) reasoningUpdate(msg tea.Msg) (tea.Cmd, bool) {
	if result0, result1, handled := reasoningUpdateTeaMouseMsg(m, msg); handled {
		return result0, result1
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, false
	}
	if key.String() == "ctrl+e" && !m.reasoningOpen {
		model := m.reasoningModel()
		if len(model.ReasoningChoices()) == 0 || !model.CanReason {
			m.note = config.Text("Níveis de raciocínio não informados para o modelo ativo", "Reasoning levels not reported for the active model")
			return nil, true
		}
		m.reasoningOpen = true
		m.reasoningIndex = max(0, slices.Index(model.ReasoningChoices(), config.Get().Agents[reasoningRole(m.target)].ReasoningEffort))
		return nil, true
	}
	if !m.reasoningOpen {
		return nil, false
	}
	levels := m.reasoningModel().ReasoningChoices()
	if len(levels) == 0 {
		m.reasoningOpen = false
		return nil, true
	}
	m.reasoningIndex = max(0, min(m.reasoningIndex, len(levels)-1))
	switch key.String() {
	case "esc":
		m.reasoningOpen = false
	case "up":
		m.reasoningIndex = max(0, m.reasoningIndex-1)
	case "down":
		m.reasoningIndex = min(len(levels)-1, m.reasoningIndex+1)
	case "enter":
		return util.CmdHandler(ReasoningSelectedMsg{Agent: reasoningRole(m.target), Model: m.reasoningModel().ID, Effort: levels[m.reasoningIndex]}), true
	}
	return nil, true
}
func (m *modelDialogCmp) reasoningView() string {
	levelsNow := m.reasoningModel().ReasoningChoices()
	m.reasoningIndex = max(0, min(m.reasoningIndex, len(levelsNow)-1))
	lines := []string{config.Text("Esforço do modelo ativo", "Active model reasoning effort"), config.Text("Mais esforço pode aumentar tempo e consumo.", "Higher effort may increase latency and usage.")}
	levels := m.reasoningModel().ReasoningChoices()
	rows, start := m.reasoningRows()
	for i := start; i < min(len(levels), start+rows); i++ {
		lines = append(lines, selectionMark(i == m.reasoningIndex)+levels[i])
	}
	width := 52
	if m.width > 0 {
		width = max(1, min(width, m.width-6))
	}
	lines = append(lines, styles.PanelDivider(width), config.Text("↑↓ escolher · Enter salvar · Esc voltar", "↑↓ select · Enter save · Esc back"))
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return styles.Panel().Padding(1, 2).Border(lipgloss.NormalBorder()).Render(strings.Join(lines, "\n"))
}

func reasoningRole(role config.AgentName) config.AgentName {
	if role == "" {
		return config.AgentCoder
	}
	return role
}

func (m *modelDialogCmp) reasoningRows() (rows, start int) {
	height := m.height
	if height <= 0 {
		height = 20
	}
	rows = max(1, height-9)
	start = max(0, m.reasoningIndex-rows+1)
	return
}

func reasoningUpdateTeaMouseMsg(m *modelDialogCmp, msg tea.Msg) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	if mouse, ok := msg.(tea.MouseMsg); ok && m.reasoningOpen {
		levels := m.reasoningModel().ReasoningChoices()
		if mouse.Mouse().Button == tea.MouseWheelUp {
			m.reasoningIndex = max(0, m.reasoningIndex-1)
		}
		if mouse.Mouse().Button == tea.MouseWheelDown {
			m.reasoningIndex = min(max(0, len(levels)-1), m.reasoningIndex+1)
		}
		rows, start := m.reasoningRows()
		index := start + mouse.Mouse().Y - 4
		if leftClick(mouse) && mouse.Mouse().X >= 3 && mouse.Mouse().X < max(4, m.width-3) && mouse.Mouse().Y >= 4 && mouse.Mouse().Y < 4+rows && index < len(levels) {
			m.reasoningIndex = index
		}
		return nil, true, true
	}
	return extractedResult0, extractedResult1, false
}
