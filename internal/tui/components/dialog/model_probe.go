package dialog

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diagnostic"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

type ModelProbeResultMsg struct {
	request uint64
	modelID string
	err     error
}

func (m *modelDialogCmp) startModelProbe() tea.Cmd {
	if len(m.filtered) == 0 {
		return nil
	}
	model := config.ModelWithProviderDefaults(m.filtered[m.selectedIdx])
	connection, err := config.ProviderForModel(model)
	if err != nil {
		m.note = config.Text("Conexão não configurada; salve o provedor primeiro.", "Connection not configured; save the provider first.")
		return nil
	}
	m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	m.cancel = cancel
	m.probeRunning = true
	request := m.request
	return func() tea.Msg {
		defer cancel()
		err := diagnostic.ProbeModel(ctx, model, connection)
		return ModelProbeResultMsg{request: request, modelID: string(model.ID), err: err}
	}
}
func (m *modelDialogCmp) modelProbeUpdate(msg tea.Msg) (tea.Cmd, bool) {
	if result, ok := msg.(ModelProbeResultMsg); ok {
		if result.request != m.request {
			return nil, true
		}
		m.probeRunning = false
		m.cancel = nil
		m.search.Focus()
		status := config.Text("Ferramenta estruturada confirmada; nenhuma ferramenta executada.", "Structured tool call confirmed; no tool executed.")
		if result.err != nil {
			status = config.Text("Teste não confirmou ferramentas; veja configuração ou tente outro modelo.", "Probe did not confirm tools; check settings or try another model.")
		}
		m.note = status
		return nil, true
	}
	if result0, result1, handled := modelProbeUpdateTeaKeyMsg(m, msg); handled {
		return result0, result1
	}
	if _, ok := msg.(tea.MouseMsg); ok && (m.probeConfirm || m.probeRunning) {
		return nil, true
	}
	return nil, false
}
func (m *modelDialogCmp) probeView() string {
	width := m.contentWidth()
	base := styles.BaseStyle()
	lines := []string{config.Text("Teste real de ferramentas", "Real tool capability probe")}
	if len(m.filtered) > 0 {
		lines = append(lines, fmt.Sprintf(config.Text("Modelo: %s", "Model: %s"), m.filtered[m.selectedIdx].APIModel))
	}
	lines = append(lines, config.Text("Inferência real · até 256 tokens.", "Real inference · up to 256 tokens."), config.Text("Pode haver cobrança do provedor.", "Provider charges may apply."), config.Text("Nenhum arquivo ou comando executado.", "No file or command executed."))
	if m.probeRunning {
		lines = append(lines, config.Text("Testando… Esc cancela.", "Testing… Esc cancels."))
	} else {
		lines = append(lines, config.Text("Enter testa · Esc volta", "Enter tests · Esc back"))
	}
	var rendered []string
	for i, line := range lines {
		line = ansi.Strip(line)
		line = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, line)
		if i == 1 {
			rendered = append(rendered, ansi.Truncate(line, width, "…"))
		} else {
			rendered = append(rendered, ansi.Wrap(line, width, ""))
		}
	}
	frame := base.Padding(m.geometry().padY, 2).Border(lipgloss.RoundedBorder()).BorderForeground(theme.CurrentTheme().BorderNormal()).Render(joinLines(rendered))
	return lipgloss.NewStyle().MaxWidth(width + 6).MaxHeight(m.geometry().height).Render(frame)
}

func modelProbeUpdateTeaKeyMsg(m *modelDialogCmp, msg tea.Msg) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if m.probeRunning {
			if key.Type == tea.KeyEsc {
				m.Close()
				m.search.Focus()
				m.note = config.Text("Teste cancelado; seleção preservada.", "Probe cancelled; selection preserved.")
			}
			return nil, true, true
		}
		if result0, result1, result2, handled := modelProbeUpdateTeaKeyMsgProbeConfirm(m, &key); handled {
			return result0, result1, result2
		}
		if key.String() == "ctrl+t" && len(m.filtered) > 0 {
			m.probeConfirm = true
			m.search.Blur()
			return nil, true, true
		}
	}
	return extractedResult0, extractedResult1, false
}

func modelProbeUpdateTeaKeyMsgProbeConfirm(m *modelDialogCmp, key *tea.KeyMsg) (extractedResult0 tea.Cmd, extractedResult1 bool, extractedResult2 bool, handled bool) {
	if m.probeConfirm {
		if key.Type == tea.KeyEsc {
			m.probeConfirm = false
			m.search.Focus()
			return nil, true, true, true
		}
		if key.Type == tea.KeyEnter {
			return m.startModelProbe(), true, true, true
		}
		return nil, true, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}
