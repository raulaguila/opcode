package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func selectionMark(selected bool) string {
	if selected {
		return "› "
	}
	return "  "
}
func clickedLabel(view, label string, m tea.MouseMsg) bool {
	if !leftClick(m) {
		return false
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	if m.Mouse().Y < 0 || m.Mouse().Y >= len(lines) {
		return false
	}
	line := lines[m.Mouse().Y]
	before, _, ok := strings.Cut(line, label)
	if !ok {
		return false
	}
	x := ansi.StringWidth(before)
	return m.Mouse().X >= x && m.Mouse().X < x+ansi.StringWidth(label)
}
