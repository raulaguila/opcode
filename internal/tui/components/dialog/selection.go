package dialog

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func selectionMark(selected bool) string {
	if selected {
		return "› "
	}
	return "  "
}
func clickedLabel(view, label string, m tea.MouseMsg) bool {
	if m.Button != tea.MouseButtonLeft || m.Action != tea.MouseActionPress {
		return false
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	if m.Y < 0 || m.Y >= len(lines) {
		return false
	}
	line := lines[m.Y]
	before, _, ok := strings.Cut(line, label)
	if !ok {
		return false
	}
	x := ansi.StringWidth(before)
	return m.X >= x && m.X < x+ansi.StringWidth(label)
}
