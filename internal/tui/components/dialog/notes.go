package dialog

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"strings"
)

func (m *providerDialogCmp) noteUpdate(msg tea.Msg) bool {
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.String() == "ctrl+d" && m.note != "" && m.screen != "list" {
			m.noteFull = !m.noteFull
			m.noteOffset = 0
			return true
		}
		if m.noteFull {
			switch k.String() {
			case "esc":
				m.noteFull = false
			case "up", "pgup":
				m.noteOffset = max(0, m.noteOffset-1)
			case "down", "pgdown":
				m.noteOffset++
			}
			return true
		}
	}
	if m.noteFull {
		switch event := msg.(type) {
		case tea.WindowSizeMsg:
			m.width, m.height = event.Width, event.Height
			return true
		case tea.MouseMsg:
			if event.Button == tea.MouseButtonWheelUp {
				m.noteOffset = max(0, m.noteOffset-1)
			}
			if event.Button == tea.MouseButtonWheelDown {
				m.noteOffset++
			}
			return true
		}
	}
	return false
}
func (m *providerDialogCmp) noteLines(note string) []string {
	lines := strings.Split(ansi.Wrap(interfaceCopy(note), m.contentWidth(), ""), "\n")
	if len(lines) > 2 {
		return []string{lines[0], config.Text("Ctrl+D detalhes completos", "Ctrl+D full details")}
	}
	return lines
}
func (m *providerDialogCmp) noteView() string {
	width := m.contentWidth()
	rows := max(1, m.height-7)
	lines := strings.Split(ansi.Wrap(interfaceCopy(m.note), width, ""), "\n")
	m.noteOffset = min(m.noteOffset, max(0, len(lines)-rows))
	body := append([]string{config.Text("Detalhes", "Details")}, lines[m.noteOffset:min(len(lines), m.noteOffset+rows)]...)
	body = append(body, config.Text("↑↓ rola · Esc volta", "↑↓ scroll · Esc back"))
	return styles.BaseStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).Width(width + 4).Render(strings.Join(body, "\n"))
}
