package ui

import (
	"bytes"
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/clipboard"
	"github.com/rivo/uniseg"
)

type chatPoint struct{ x, row int }
type chatSelection struct {
	anchor, focus   chatPoint
	dragging, moved bool
	session         string
	notice          string
	saved           string // the text, once the rows under it changed
}

func (m *Model) selectionMouse(msg tea.MouseMsg) bool {
	if m.dialog != nil {
		return false
	}
	mouse := msg.Mouse()
	if _, ok := msg.(tea.MouseClickMsg); ok && mouse.Button == tea.MouseLeft {
		if mouse.Y < headerRows || mouse.Y >= headerRows+m.transcript.Height() {
			m.clearSelection()
			return false
		}
		m.clearSelection()
		point := m.chatPoint(mouse.X, mouse.Y)
		m.selection = &chatSelection{anchor: point, focus: point, dragging: true, session: m.session.ID}
		m.follow = false
		return true
	}
	if m.selection == nil || !m.selection.dragging {
		return false
	}
	switch msg.(type) {
	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		if mouse.Y < headerRows {
			m.transcript.ScrollUp(1)
		}
		if mouse.Y >= headerRows+m.transcript.Height() {
			m.transcript.ScrollDown(1)
		}
		m.selection.focus = m.chatPoint(mouse.X, mouse.Y)
		m.selection.moved = m.selection.moved || m.selection.focus != m.selection.anchor
		if _, released := msg.(tea.MouseReleaseMsg); released {
			m.selection.dragging = false
			if !m.selection.moved {
				m.clearSelection()
			}
		}
		return true
	}
	return false
}

func (m *Model) chatPoint(x, y int) chatPoint {
	row := m.transcript.YOffset() + max(0, min(y-headerRows, m.transcript.Height()-1))
	return chatPoint{max(0, min(x, m.width-1)), max(0, min(row, len(m.rows)-1))}
}

func (m *Model) clearSelection() {
	if m.selection == nil {
		return
	}
	m.selection = nil
	m.refresh()
}

// holdSelection lets the transcript keep updating under a selection: when new
// rows differ from the selected ones the highlight goes and the text stays.
func (m *Model) holdSelection(next []string) {
	s := m.selection
	if s == nil || !s.moved || s.saved != "" {
		return
	}
	a, b := min(s.anchor.row, s.focus.row), max(s.anchor.row, s.focus.row)
	if b < len(m.rows) && b < len(next) && slices.Equal(m.rows[a:b+1], next[a:b+1]) {
		return
	}
	s.saved, s.dragging = m.selectedText(), false
}

func (m *Model) selectedCells(row int, line string) (int, int) {
	if m.selection == nil || !m.selection.moved || m.selection.saved != "" {
		return 0, 0
	}
	a, b := m.selection.anchor, m.selection.focus
	if a.row > b.row || a.row == b.row && a.x > b.x {
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
	g, x := uniseg.NewGraphemes(ansi.Strip(line)), 0
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

func (m *Model) selectedText() string {
	if m.selection == nil || !m.selection.moved {
		return ""
	}
	if m.selection.saved != "" {
		return m.selection.saved
	}
	a, b := m.selection.anchor.row, m.selection.focus.row
	if a > b {
		a, b = b, a
	}
	var rows []string
	for row := a; row <= b && row < len(m.rows); row++ {
		left, right := m.selectedCells(row, m.rows[row])
		rows = append(rows, strings.TrimRight(ansi.Strip(ansi.Cut(m.rows[row], left, right)), " "))
	}
	return strings.Join(rows, "\n")
}

func (m *Model) highlightSelection(rows []string, from int) []string {
	if m.selection == nil || !m.selection.moved {
		return rows
	}
	out := append([]string(nil), rows...)
	for i, line := range out {
		left, right := m.selectedCells(from+i, line)
		if right > left {
			out[i] = ansi.Cut(line, 0, left) + "\x1b[7m" + ansi.Strip(ansi.Cut(line, left, right)) + "\x1b[27m" + ansi.Cut(line, right, ansi.StringWidth(line))
		}
	}
	return out
}

type chatCopyMsg struct {
	sequence  string
	confirmed bool
	err       error
}

func copyChatText(text string) tea.Cmd {
	return func() tea.Msg {
		var terminal bytes.Buffer
		confirmed, err := clipboard.Copy(context.Background(), text, &terminal)
		return chatCopyMsg{terminal.String(), confirmed, err}
	}
}

func (m *Model) copiedChat(msg chatCopyMsg) tea.Cmd {
	status := textFor("Cópia enviada ao terminal; recebimento não confirmado", "Copy sent to terminal; receipt unconfirmed")
	if msg.confirmed {
		status = textFor("Copiado", "Copied")
	}
	if msg.err != nil {
		status = msg.err.Error()
	}
	if m.selection != nil {
		m.selection.notice = status
	}
	if d, ok := m.dialog.(*commandDialog); ok {
		d.notice = status
	}
	if msg.err != nil {
		m.notice = msg.err.Error()
		return nil
	}
	if msg.confirmed {
		m.info = textFor("Seleção copiada", "Selection copied")
		return nil
	}
	m.info = textFor("Cópia enviada ao terminal; recebimento não confirmado", "Copy sent to terminal; receipt unconfirmed")
	if clipboard.ValidOSC52(msg.sequence) {
		return tea.Raw(msg.sequence)
	}
	return nil
}
