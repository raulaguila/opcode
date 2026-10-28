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
		status := config.Text("Ferramenta estruturada confirmada; nenhuma ferramenta executada.", "Structured tool call confirmed; no tool executed.")
		if result.err != nil {
			status = config.Text("Teste não confirmou ferramentas; veja configuração ou tente outro modelo.", "Probe did not confirm tools; check settings or try another model.")
		}
		m.note = status
		return nil, true
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		if m.probeRunning {
			if key.Type == tea.KeyEsc {
				m.Close()
				m.note = config.Text("Teste cancelado; seleção preservada.", "Probe cancelled; selection preserved.")
			}
			return nil, true
		}
		if m.probeConfirm {
			if key.Type == tea.KeyEsc {
				m.probeConfirm = false
				return nil, true
			}
			if key.Type == tea.KeyEnter {
				return m.startModelProbe(), true
			}
			return nil, true
		}
		if key.String() == "ctrl+t" && len(m.filtered) > 0 {
			m.probeConfirm = true
			return nil, true
		}
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
	return base.Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(theme.CurrentTheme().TextMuted()).Render(joinLines(rendered))
}
