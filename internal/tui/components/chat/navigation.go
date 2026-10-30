package chat

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/rivo/uniseg"
	"strings"
	"unicode"
)

// TranscriptNavigation only navigates local presentation, never model context.
type TranscriptNavigation interface {
	TranscriptKey(tea.KeyMsg) bool
	StartSearch(string)
	ToggleKeyboardSelection()
}
type transcriptMatch struct{ row, left, right int }
type transcriptFind struct {
	open, limited   bool
	query           string
	matches         []transcriptMatch
	selected        int
	normalized      []searchLine
	normalizedBytes int
}

func (m *messagesCmp) TranscriptKey(msg tea.KeyMsg) bool {
	if msg.Paste && !m.find.open {
		return false
	}
	// Existing custom global shortcuts retain precedence. Slash commands still
	// expose both actions when these optional defaults are already occupied.
	if result0, handled := transcriptKeyAltF(&msg); handled {
		return result0
	}
	switch msg.String() {
	case "alt+f":
		if m.find.open {
			m.find.open = false
		} else {
			m.StartSearch(m.find.query)
		}
		return true
	case "alt+s":
		m.ToggleKeyboardSelection()
		return true
	}
	if m.find.open {
		return m.findKey(msg)
	}
	if !m.selection.keyboard {
		return false
	}
	key := msg.String()
	if key == "esc" {
		m.ClearSelection()
		return true
	}
	extend := strings.HasPrefix(key, "shift+")
	key = strings.TrimPrefix(key, "shift+")
	point := m.selection.focus
	switch key {
	case "left":
		point = m.horizontalPoint(point, -1)
	case "right":
		point = m.horizontalPoint(point, 1)
	case "up":
		point.row = max(0, point.row-1)
	case "down":
		point.row = min(len(m.transcriptLines)-1, point.row+1)
	case "home":
		point.x = 0
	case "end":
		point.x = max(0, ansi.StringWidth(strings.TrimRight(ansi.Strip(m.transcriptLines[point.row]), " "))-1)
	case "pgup":
		point.row = max(0, point.row-max(1, m.viewport.Height))
	case "pgdown":
		point.row = min(len(m.transcriptLines)-1, point.row+max(1, m.viewport.Height))
	default:
		return false
	}
	point.x = min(point.x, max(0, ansi.StringWidth(m.transcriptLines[point.row])-1))
	point.x, _ = selectionCells(m.transcriptLines[point.row], point.x, point.x+1)
	m.selection.focus = point
	if !extend {
		m.selection.anchor = point
	}
	m.selection.moved = true
	m.revealRow(point.row)
	return true
}
func (m *messagesCmp) horizontalPoint(p textPoint, direction int) textPoint {
	plain := ansi.Strip(m.transcriptLines[p.row])
	g := uniseg.NewGraphemes(plain)
	x, previous := 0, 0
	for g.Next() {
		next := x + ansi.StringWidth(g.Str())
		if p.x < next {
			if direction > 0 {
				if next >= ansi.StringWidth(plain) && p.row+1 < len(m.transcriptLines) {
					return textPoint{0, p.row + 1}
				}
				return textPoint{min(next, max(0, ansi.StringWidth(plain)-1)), p.row}
			}
			if x == 0 && p.row > 0 {
				return textPoint{max(0, ansi.StringWidth(m.transcriptLines[p.row-1])-1), p.row - 1}
			}
			return textPoint{previous, p.row}
		}
		previous, x = x, next
	}
	return p
}
func (m *messagesCmp) ToggleKeyboardSelection() {
	if m.selection.keyboard {
		m.ClearSelection()
		return
	}
	m.loadEarlierHistory(true)
	var point textPoint
	found := false
	if m.find.open && len(m.find.matches) > 0 {
		hit := m.find.matches[m.find.selected]
		point, found = textPoint{hit.left, hit.row}, true
	}
	m.find.open = false
	m.ClearSelection()
	if !found {
		for row := m.viewport.YOffset; row < len(m.transcriptLines); row++ {
			plain := ansi.Strip(m.transcriptLines[row])
			rest := strings.TrimLeftFunc(plain, func(r rune) bool { return unicode.IsSpace(r) || r == '┃' || r == '│' })
			if rest != "" {
				point, found = textPoint{ansi.StringWidth(plain) - ansi.StringWidth(rest), row}, true
				break
			}
		}
	}
	if !found || m.rendering || len(m.messages) == 0 {
		return
	}
	m.selection = textSelection{anchor: point, focus: point, moved: true, keyboard: true}
	m.revealRow(point.row)
}
func (m *messagesCmp) StartSearch(query string) {
	m.ClearSelection()
	m.loadEarlierHistory(true)
	m.find = transcriptFind{open: true, query: boundedQuery(query)}
	m.updateSearch()
	m.jumpSearch(0)
}
func boundedQuery(s string) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s))
	if len(runes) > 128 {
		runes = runes[:128]
	}
	return string(runes)
}
func (m *messagesCmp) updateSearch() {
	m.find.matches = nil
	m.find.limited = false
	query := strings.ToLower(m.find.query)
	if query == "" {
		m.find.selected = 0
		return
	}
	bytes := 0
	for row, line := range m.transcriptLines {
		bytes += len(line)
		if row >= 10000 || bytes > 2<<20 {
			m.find.limited = true
			break
		}
		plain := m.normalizedSearchLine(row, line)
		for offset := 0; offset < len(plain); {
			at := strings.Index(plain[offset:], query)
			if at < 0 {
				break
			}
			at += offset
			left := ansi.StringWidth(plain[:at])
			right := left + ansi.StringWidth(plain[at:at+len(query)])
			m.find.matches = append(m.find.matches, transcriptMatch{row, left, right})
			if len(m.find.matches) >= 200 {
				m.find.limited = true
				break
			}
			offset = at + len(query)
		}
		if m.find.limited {
			break
		}
	}
	m.find.selected = max(0, min(m.find.selected, len(m.find.matches)-1))
}
func (m *messagesCmp) findKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc":
		m.find.open = false
		return true
	case "enter", "down", "tab":
		m.jumpSearch(1)
		return true
	case "up", "shift+tab", "ctrl+j":
		m.jumpSearch(-1)
		return true
	case "ctrl+c":
		return true
	case "ctrl+u":
		m.find.query = ""
	case "backspace", "ctrl+h":
		r := []rune(m.find.query)
		if len(r) > 0 {
			m.find.query = string(r[:len(r)-1])
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.find.query = boundedQuery(m.find.query + string(msg.Runes))
		} else {
			return true
		}
	}
	m.find.selected = 0
	m.updateSearch()
	m.jumpSearch(0)
	return true
}
func (m *messagesCmp) jumpSearch(delta int) {
	if len(m.find.matches) == 0 {
		return
	}
	m.find.selected = (m.find.selected + delta + len(m.find.matches)) % len(m.find.matches)
	m.revealRow(m.find.matches[m.find.selected].row)
}
func (m *messagesCmp) revealRow(row int) {
	if row < m.viewport.YOffset {
		m.viewport.SetYOffset(row)
	}
	if row >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(row - max(1, m.viewport.Height) + 1)
	}
}
func (m *messagesCmp) navigationHint() string {
	if m.find.open {
		position := 0
		if len(m.find.matches) > 0 {
			position = m.find.selected + 1
		}
		note := fmt.Sprintf(config.Text("Buscar: %s · %d/%d · ↑↓/Enter · Esc", "Find: %s · %d/%d · ↑↓/Enter · Esc"), m.find.query, position, len(m.find.matches))
		if m.find.limited {
			note += config.Text(" · resultados limitados", " · limited results")
		}
		return ansi.Truncate(note, m.width, "…")
	}
	return ansi.Truncate(config.Text("Setas movem · Shift+setas amplia · Ctrl+C copia · Esc", "Arrows move · Shift+arrows extend · Ctrl+C copy · Esc"), m.width, "…")
}

func transcriptKeyAltF(msg *tea.KeyMsg) (extractedResult0 bool, handled bool) {
	if msg.String() == "alt+f" || msg.String() == "alt+s" {
		for action := range config.DefaultKeybindings {
			if config.Shortcut(action) == msg.String() {
				return false, true
			}
		}
	}
	return extractedResult0, false
}
