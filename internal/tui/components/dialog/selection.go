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
	i := strings.Index(line, label)
	if i < 0 {
		return false
	}
	x := ansi.StringWidth(line[:i])
	return m.X >= x && m.X < x+ansi.StringWidth(label)
}
