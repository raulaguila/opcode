package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/rivo/uniseg"
)

type SelectionControl interface {
	HasSelection() bool
	SelectedText() string
	ClearSelection()
	SelectionDragging() bool
}

type textPoint struct{ x, row int }
type textSelection struct {
	anchor, focus   textPoint
	dragging, moved bool
	keyboard        bool
}

func (s textSelection) engaged() bool          { return s.dragging || s.moved }
func (m *messagesCmp) SelectionDragging() bool { return m.selection.dragging }
func (m *messagesCmp) HasSelection() bool      { return m.selection.moved }

func (m *messagesCmp) ClearSelection() {
	active := m.selection.engaged()
	m.selection = textSelection{}
	if active {
		offset := m.viewport.YOffset
		m.renderView()
		m.viewport.SetYOffset(offset)
	}
}

func (m *messagesCmp) selectionMouse(msg tea.MouseMsg) {
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		m.ClearSelection()
		row := msg.Y - 1 + m.viewport.YOffset // Container padding.
		if m.rendering || len(m.messages) == 0 || row < 0 || row >= len(m.transcriptLines) || msg.Y < 1 || msg.Y > m.viewport.Height {
			return
		}
		point := textPoint{max(0, min(msg.X-1, m.width-1)), row}
		m.selection = textSelection{anchor: point, focus: point, dragging: true}
		return
	}
	if !m.selection.dragging || (msg.Action != tea.MouseActionMotion && msg.Action != tea.MouseActionRelease) {
		return
	}
	row := max(0, min(msg.Y-1, m.viewport.Height-1)) + m.viewport.YOffset
	point := textPoint{max(0, min(msg.X-1, m.width-1)), min(row, len(m.transcriptLines)-1)}
	m.selection.focus = point
	m.selection.moved = m.selection.moved || point != m.selection.anchor
	if msg.Action == tea.MouseActionRelease {
		if !m.selection.moved {
			m.ClearSelection()
		} else {
			m.selection.dragging = false
		}
	}
}

// Endpoints refer to display cells. Expand partial wide/combining characters to
// complete graphemes, so a drag never copies half an emoji or CJK character.
func selectionCells(line string, left, right int) (int, int) {
	g := uniseg.NewGraphemes(ansi.Strip(line))
	x := 0
	for g.Next() {
		next := x + ansi.StringWidth(g.Str())
		if left > x && left < next {
			left = x
		}
		if right > x && right < next {
			right = next
		}
		x = next
	}
	return min(left, x), min(right, x)
}

func (m *messagesCmp) selectedCells(row int, line string) (int, int) {
	if !m.selection.moved {
		if m.find.open && len(m.find.matches) > 0 {
			hit := m.find.matches[m.find.selected]
			if row == hit.row {
				return selectionCells(line, hit.left, hit.right)
			}
		}
		return 0, 0
	}
	a, b := m.selection.anchor, m.selection.focus
	if a.row > b.row || (a.row == b.row && a.x > b.x) {
		a, b = b, a
	}
	if row < a.row || row > b.row {
		return 0, 0
	}
	left, right := 0, ansi.StringWidth(line)
	if row == a.row {
		left = a.x
	}
	if row == b.row {
		right = b.x + 1
	}
	return selectionCells(line, left, right)
}

func (m *messagesCmp) SelectedText() string {
	if !m.selection.moved {
		return ""
	}
	var lines []string
	a, b := m.selection.anchor.row, m.selection.focus.row
	if a > b {
		a, b = b, a
	}
	for row := a; row <= b && row < len(m.transcriptLines); row++ {
		line := m.transcriptLines[row]
		left, right := m.selectedCells(row, line)
		lines = append(lines, strings.TrimRight(ansi.Strip(ansi.Cut(line, left, right)), " "))
	}
	return strings.Join(lines, "\n")
}

func (m *messagesCmp) highlightSelection(content string) string {
	if !m.selection.moved && !(m.find.open && len(m.find.matches) > 0) {
		return content
	}
	lines := strings.Split(content, "\n")
	style := styles.BaseStyle().Reverse(true)
	for i, line := range lines {
		left, right := m.selectedCells(i+m.viewport.YOffset, line)
		if right <= left {
			continue
		}
		span := ansi.Strip(ansi.Cut(line, left, right))
		highlight := style.Render(span)
		if lipgloss.ColorProfile() == termenv.Ascii {
			// NO_COLOR disables the palette, not text selection. Lipgloss also
			// drops Reverse in its ASCII profile; retain this color-free attribute.
			highlight = "\x1b[7m" + span + "\x1b[27m"
		}
		lines[i] = ansi.Cut(line, 0, left) + highlight + ansi.Cut(line, right, lipgloss.Width(line))
	}
	return strings.Join(lines, "\n")
}
