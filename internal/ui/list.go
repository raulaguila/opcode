package ui

import (
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Item is one row of a List. Group heads a run of rows; Right sits at the
// row's end (a shortcut, a size); Mark sits before the title (✓, ●).
type Item struct {
	ID, Title, Desc, Right, Group, Mark string
	MarkColor                           color.Color // Success when nil
	Faint                               bool
	Value                               any
}

// List is the proposal's searchable list: a search field, uppercase group
// headings while unfiltered, and the selection drawn with a bar and a tint.
type List struct {
	Items    []Item
	Query    string
	Selected int
	Offset   int
	Grouped  bool
	Cursor   string // drawn in the mark column of the selected row ("◆")
	Chips    bool   // Right is a key, drawn as a chip
}

// Visible is the filtered items, in order.
func (l *List) Visible() []Item {
	query := strings.ToLower(strings.TrimSpace(l.Query))
	if query == "" {
		return l.Items
	}
	// Title matches lead; description and ID matches follow.
	var titles, others []Item
	for _, item := range l.Items {
		switch {
		case strings.Contains(strings.ToLower(item.Title), query):
			titles = append(titles, item)
		case strings.Contains(strings.ToLower(item.Desc+" "+item.ID+" "+item.Right), query):
			others = append(others, item)
		}
	}
	return append(titles, others...)
}

// Current is the selected item, if any.
func (l *List) Current() (Item, bool) {
	items := l.Visible()
	if l.Selected < 0 || l.Selected >= len(items) {
		return Item{}, false
	}
	return items[l.Selected], true
}

func (l *List) Move(delta, rows int) {
	items := l.Visible()
	if len(items) == 0 {
		l.Selected = 0
		return
	}
	l.Selected = (l.Selected + delta + len(items)) % len(items)
	l.keepVisible(rows)
}

func (l *List) SetQuery(query string) {
	l.Query, l.Selected, l.Offset = query, 0, 0
}

// layout maps rows to items: -1 is a group heading, -2 the blank row
// that separates one group from the one above.
func (l *List) layout(rows int) []int {
	items := l.Visible()
	grouped := l.Grouped && strings.TrimSpace(l.Query) == ""
	var out []int
	for i := l.Offset; i < len(items) && len(out) < rows; i++ {
		if grouped && items[i].Group != "" && (i == l.Offset || items[i-1].Group != items[i].Group) {
			head := []int{-1}
			if len(out) > 0 {
				head = []int{-2, -1}
			}
			if len(out)+len(head) >= rows { // a heading never ends the list
				break
			}
			out = append(out, head...)
		}
		out = append(out, i)
	}
	return out
}

func (l *List) keepVisible(rows int) {
	if l.Selected < l.Offset {
		l.Offset = l.Selected
	}
	for l.Offset < l.Selected && !contains(l.layout(rows), l.Selected) {
		l.Offset++
	}
}

func contains(rows []int, want int) bool {
	for _, row := range rows {
		if row == want {
			return true
		}
	}
	return false
}

// ItemAt is the item drawn at a list row (zero-based), for mouse clicks.
func (l *List) ItemAt(row, rows int) (int, bool) {
	layout := l.layout(rows)
	if row < 0 || row >= len(layout) || layout[row] < 0 {
		return 0, false
	}
	return layout[row], true
}

// Render draws up to rows list rows at the given width on the surface.
func (l *List) Render(width, rows int) []string {
	items := l.Visible()
	if len(items) == 0 {
		return []string{on(P.Surface, P.Muted).Render("  " + empty())}
	}
	layout := l.layout(rows)
	marks := l.Cursor != ""
	for _, item := range items {
		marks = marks || item.Mark != ""
	}
	// Titles share one column, so the descriptions line up after them.
	column := 0
	for _, i := range layout {
		if i >= 0 && items[i].Desc != "" {
			column = max(column, ansi.StringWidth(items[i].Title))
		}
	}
	column = min(column, width/2)
	out := make([]string, 0, len(layout))
	for k, i := range layout {
		switch {
		case i == -2:
			out = append(out, "")
		case i == -1:
			out = append(out, on(P.Surface, P.Muted).Render("  "+strings.ToUpper(items[layout[k+1]].Group)))
		default:
			out = append(out, l.row(items[i], i == l.Selected, marks, width, column))
		}
	}
	return out
}

func (l *List) row(item Item, selected, marks bool, width, column int) string {
	bg, fg := P.Surface, P.Text
	if item.Faint {
		fg = P.Faint
	}
	bar := on(bg, bg).Render("  ")
	if selected {
		bg, fg = P.Selected, P.Strong
		bar = on(P.Surface, P.Ask).Render("▌") + on(bg, bg).Render(" ")
	}
	// Rows line up past a shared mark column, as in the proposal.
	mark := ""
	switch {
	case item.Mark != "":
		markColor := item.MarkColor
		if markColor == nil {
			markColor = P.Success
		}
		mark = on(bg, markColor).Render(item.Mark) + on(bg, fg).Render(" ")
	case selected && l.Cursor != "":
		mark = on(bg, P.Ask).Render(l.Cursor) + on(bg, fg).Render(" ")
	case marks:
		mark = on(bg, bg).Render("  ")
	}
	title := on(bg, fg).Bold(selected).Render(item.Title)
	if query := strings.TrimSpace(l.Query); query != "" {
		if at := strings.Index(strings.ToLower(item.Title), strings.ToLower(query)); at >= 0 {
			title = on(bg, fg).Bold(selected).Render(item.Title[:at]) + on(bg, fg).Bold(selected).Underline(true).Render(item.Title[at:at+len(query)]) + on(bg, fg).Bold(selected).Render(item.Title[at+len(query):])
		}
	}
	left := bar + mark + title
	if item.Desc != "" {
		gap := max(0, column-ansi.StringWidth(item.Title))
		left += on(bg, P.Muted).Render(strings.Repeat(" ", gap+3) + item.Desc)
	}
	right := ""
	switch {
	case item.Right != "" && l.Chips:
		right = on(P.Raised, P.Muted).Render(" "+item.Right+" ") + on(bg, bg).Render(" ")
	case item.Right != "":
		right = on(bg, P.Muted).Render(item.Right) + on(bg, bg).Render(" ")
	}
	return Spread(left, right, width, bg)
}

func empty() string { return textFor("Nada encontrado", "Nothing found") }
