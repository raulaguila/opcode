package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/layout"
)

func textFor(pt, en string) string { return config.Text(pt, en) }

// listDialog is a framed List that floats over the screen: the palette, the
// model picker and the conversations are all one of these.
type listDialog struct {
	title, legend string
	meta          func(*listDialog) string
	list          List
	width         int
	searchPrompt  string
	placeholder   string
	noSearch      bool // the details view lists without a field
	split         int  // percent of the width the list takes beside a side column; 45 by default
	// header adds rows between search and list (tabs); side draws a column
	// beside the list (details, a preview); choose acts on Enter.
	header func(*Model, int) []string
	side   func(*Model, Item, int) []string
	choose func(*Model, Item) tea.Cmd
	keys   func(*Model, tea.KeyPressMsg) (tea.Cmd, bool)
}

const listRows = 12

func (d *listDialog) view(m *Model) string {
	width := min(d.width, m.width-4)
	box := Box{Title: d.title, Width: width, PadX: 2, PadY: 1}
	if d.meta != nil {
		box.Meta = d.meta(d)
	}
	inner := box.Inner()
	listWidth := inner
	var side []string
	if current, ok := d.list.Current(); ok && d.side != nil && inner >= 70 {
		split := d.split
		if split == 0 {
			split = 45
		}
		listWidth = inner * split / 100
		side = d.side(m, current, inner-listWidth-3)
	}
	var rows []string
	if d.header != nil {
		rows = append(rows, d.header(m, listWidth)...)
	}
	if !d.noSearch {
		placeholder := d.placeholder
		if placeholder == "" {
			placeholder = textFor("Buscar", "Search")
		}
		field := on(P.Field, P.Ask).Render(" "+d.searchPrompt+" ") + on(P.Field, P.Strong).Render(d.list.Query) + on(P.Field, P.Ask).Render("▏")
		if d.list.Query == "" {
			field = on(P.Field, P.Ask).Render(" "+d.searchPrompt+" ") + on(P.Field, P.Faint).Render(placeholder)
		}
		rows = append(rows, Line(field, listWidth, P.Field), Line("", listWidth, P.Surface))
	}
	rows = append(rows, d.list.Render(listWidth, min(listRows, max(3, m.height-14)))...)
	for i := range rows {
		rows[i] = Line(rows[i], listWidth, P.Surface)
	}
	if side != nil {
		rule := on(P.Surface, P.Border).Render(" │ ")
		for len(rows) < len(side) {
			rows = append(rows, Line("", listWidth, P.Surface))
		}
		for i := range rows {
			right := ""
			if i < len(side) {
				right = side[i]
			}
			rows[i] += rule + right
		}
	}
	if d.legend != "" {
		rows = append(rows, "")
		rows = append(rows, KeyRow(d.legend, inner, P.Surface)...)
	}
	return box.Render(strings.Join(rows, "\n"))
}

func (d *listDialog) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if d.keys != nil {
		if cmd, done := d.keys(m, msg); done {
			return cmd, true
		}
	}
	rows := min(listRows, max(3, m.height-14))
	switch msg.String() {
	case "esc":
		m.dialog = nil
		return nil, true
	case "up":
		d.list.Move(-1, rows)
	case "down":
		d.list.Move(1, rows)
	case "enter":
		if item, ok := d.list.Current(); ok && d.choose != nil {
			return d.choose(m, item), true
		}
	case "backspace":
		if q := []rune(d.list.Query); len(q) > 0 {
			d.list.SetQuery(string(q[:len(q)-1]))
		}
	case "ctrl+u":
		d.list.SetQuery("")
	default:
		if msg.Text != "" {
			d.list.SetQuery(d.list.Query + msg.Text)
		}
	}
	return nil, true
}

func (d *listDialog) paste(text string) { d.list.SetQuery(d.list.Query + text) }

// floating is what every panel over the screen offers it.
type floating interface {
	view(*Model) string
	key(*Model, tea.KeyPressMsg) (tea.Cmd, bool)
}

// overlay centers a floating panel over the screen.
func (m *Model) overlay(base string) string {
	if m.dialog == nil {
		return base
	}
	// The screen behind recedes, as in the proposal: plain text in a faint tone.
	var dimmed []string
	for _, line := range strings.Split(ansi.Strip(base), "\n") {
		dimmed = append(dimmed, on(P.Ground, P.Dim).Render(line))
	}
	base = strings.Join(dimmed, "\n")
	panel := m.dialog.view(m)
	x := max(0, (m.width-ansi.StringWidth(strings.SplitN(panel, "\n", 2)[0]))/2)
	y := max(1, (m.height-strings.Count(panel, "\n")-1)/3)
	m.placeHosted(x, y)
	return layout.PlaceOverlay(x, y, panel, base, false)
}
