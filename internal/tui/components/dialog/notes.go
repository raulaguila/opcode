package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
)

func (m *providerDialogCmp) noteUpdate(msg tea.Msg) bool {
	if result0, handled := noteUpdateTeaKeyMsg(m, msg); handled {
		return result0
	}
	if m.noteFull {
		switch event := msg.(type) {
		case tea.WindowSizeMsg:
			m.width, m.height = event.Width, event.Height
			return true
		case tea.MouseMsg:
			if event.Mouse().Button == tea.MouseWheelUp {
				m.noteOffset = max(0, m.noteOffset-1)
			}
			if event.Mouse().Button == tea.MouseWheelDown {
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
		return []string{lines[0], dialogHint(config.Text("Ctrl+D detalhes completos", "Ctrl+D full details"), m.contentWidth())}
	}
	return lines
}
func (m *providerDialogCmp) noteView() string {
	width := m.contentWidth()
	rows := max(1, m.height-8)
	lines := strings.Split(ansi.Wrap(interfaceCopy(m.note), width, ""), "\n")
	m.noteOffset = min(m.noteOffset, max(0, len(lines)-rows))
	body := append([]string{config.Text("Detalhes", "Details")}, lines[m.noteOffset:min(len(lines), m.noteOffset+rows)]...)
	body = append(body, styles.PanelDivider(width), dialogHint(config.Text("↑↓ rola · Esc volta", "↑↓ scroll · Esc back"), width))
	return styles.Panel().Padding(1, 2).Border(lipgloss.NormalBorder()).Render(strings.Join(body, "\n"))
}

func noteUpdateTeaKeyMsg(m *providerDialogCmp, msg tea.Msg) (extractedResult0 bool, handled bool) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if k.String() == "ctrl+d" && m.note != "" && m.screen != "list" {
			m.noteFull = !m.noteFull
			m.noteOffset = 0
			return true, true
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
			return true, true
		}
	}
	return extractedResult0, false
}
