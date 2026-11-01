package ui

import (
	"context"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/transcript"
)

// sessionDay groups conversations by when they last changed.
func sessionDay(updated int64, now time.Time) string {
	if updated <= 0 {
		return textFor("Sem data", "Undated")
	}
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	switch at := time.Unix(updated, 0); {
	case !at.Before(today):
		return textFor("Hoje", "Today")
	case !at.Before(today.AddDate(0, 0, -1)):
		return textFor("Ontem", "Yesterday")
	case !at.Before(today.AddDate(0, 0, -7)):
		return textFor("Esta semana", "This week")
	}
	return textFor("Antes", "Earlier")
}

func recency(updated int64, now time.Time) string {
	elapsed := now.Sub(time.Unix(updated, 0))
	switch {
	case updated <= 0:
		return ""
	case elapsed < time.Minute:
		return textFor("agora", "now")
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	}
	return time.Unix(updated, 0).Format("02/01")
}

// sessionsDialog is the proposal's conversation list: by day, the selected
// one previewed beside, with rename, export and archive.
type sessionsDialog struct {
	*listDialog
	archived     map[string]bool
	showArchived bool
	renaming     bool
	draft        string
	previews     map[string][2]string // last prompt and last reply
}

// openSessions lists conversations by day, the selected one previewed beside.
func (m *Model) openSessions() tea.Cmd {
	if m.app == nil || m.app.Sessions == nil {
		return nil
	}
	d := &sessionsDialog{previews: map[string][2]string{}}
	d.listDialog = &listDialog{
		title:        textFor("Conversas", "Conversations"),
		searchPrompt: "⌕",
		placeholder:  textFor("Buscar conversa", "Search conversations"),
		width:        108,
		split:        40,
		meta:         func(*listDialog) string { return d.meta() },
		side:         d.preview,
		choose: func(m *Model, item Item) tea.Cmd {
			m.openSession(item.Value.(session.Session))
			return nil
		},
		keys: d.keys,
	}
	m.dialog = d
	d.reload(m, m.session.ID)
	return nil
}

func (d *sessionsDialog) catalog() *session.Catalog {
	cfg := config.Get()
	return session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory)
}

// reload reads the conversations again and keeps the selection on keep.
func (d *sessionsDialog) reload(m *Model, keep string) {
	d.legend = textFor("Enter abrir · Ctrl+R renomear · Ctrl+E exportar · Ctrl+A arquivar · Ctrl+H arquivadas · Esc fechar", "Enter open · Ctrl+R rename · Ctrl+E export · Ctrl+A archive · Ctrl+H archived · Esc close")
	list, err := m.app.Sessions.List(context.Background())
	if err != nil {
		m.notice = err.Error()
		return
	}
	d.archived = map[string]bool{}
	if snapshot, _, err := d.catalog().Snapshot(); err == nil {
		for id, state := range snapshot {
			d.archived[id] = state.Archived
		}
	}
	slices.SortFunc(list, func(a, b session.Session) int { return int(b.UpdatedAt - a.UpdatedAt) })
	now := time.Now()
	items := make([]Item, 0, len(list))
	selected := 0
	for _, s := range list {
		if s.ParentSessionID != "" || d.archived[s.ID] != d.showArchived {
			continue
		}
		item := Item{ID: s.ID, Title: s.Title, Right: recency(s.UpdatedAt, now), Group: sessionDay(s.UpdatedAt, now), Value: s, Mark: "✓"}
		switch {
		case d.archived[s.ID]:
			item.Mark, item.MarkColor = "○", P.Faint
		case m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(s.ID):
			item.Mark, item.MarkColor = "●", P.Execute
		}
		if s.ID == keep {
			selected = len(items)
		}
		items = append(items, item)
	}
	d.list = List{Items: items, Grouped: true, Query: d.list.Query, Selected: selected}
	d.list.keepVisible(min(listRows, max(3, m.height-14)))
}

func (d *sessionsDialog) meta() string {
	hidden := 0
	for _, archived := range d.archived {
		if archived {
			hidden++
		}
	}
	n := len(d.list.Visible())
	switch {
	case d.showArchived:
		return fmt.Sprintf(textFor("%d arquivadas", "%d archived"), n)
	case hidden == 0:
		return fmt.Sprint(n)
	}
	return fmt.Sprintf(textFor("%d · %d arquivadas", "%d · %d archived"), n, hidden)
}

func (d *sessionsDialog) keys(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	item, ok := d.list.Current()
	if d.renaming {
		switch msg.String() {
		case "esc":
			d.renaming = false
		case "enter":
			d.rename(m, item)
		case "backspace":
			if r := []rune(d.draft); len(r) > 0 {
				d.draft = string(r[:len(r)-1])
			}
		default:
			d.draft += msg.Text
		}
		return nil, true
	}
	switch msg.String() {
	case "ctrl+h":
		d.showArchived = !d.showArchived
		d.reload(m, "")
		return nil, true
	case "ctrl+r", "ctrl+e", "ctrl+a":
	default:
		return nil, false
	}
	if !ok {
		return nil, true
	}
	s := item.Value.(session.Session)
	if m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(s.ID) {
		m.notice = textFor("Aguarde ou cancele esta conversa antes de gerenciá-la", "Wait for or cancel this conversation before managing it")
		return nil, true
	}
	switch msg.String() {
	case "ctrl+r":
		d.renaming, d.draft = true, s.Title
	case "ctrl+e":
		cfg := config.Get()
		messages, err := m.app.Messages.List(context.Background(), s.ID)
		if err == nil {
			var path string
			if path, err = transcript.Save(cfg.Data.Directory, cfg.WorkingDir, s, messages, tools.RedactCredentials); err == nil {
				m.info = textFor("Conversa exportada: ", "Conversation exported: ") + homeRelative(path)
			}
		}
		if err != nil {
			m.notice = err.Error()
		}
	case "ctrl+a":
		if err := d.catalog().Archive(s.ID, !d.archived[s.ID]); err != nil {
			m.notice = err.Error()
			return nil, true
		}
		m.info = textFor("Conversa arquivada", "Conversation archived")
		if d.archived[s.ID] {
			m.info = textFor("Conversa restaurada", "Conversation restored")
		}
		d.reload(m, "")
	}
	return nil, true
}

func (d *sessionsDialog) rename(m *Model, item Item) {
	s := item.Value.(session.Session)
	s.Title = strings.TrimSpace(d.draft)
	if s.Title == "" || len(s.Title) > 200 {
		m.notice = textFor("Use um título entre 1 e 200 caracteres", "Use a title between 1 and 200 characters")
		return
	}
	saved, err := m.app.Sessions.Save(context.Background(), s)
	if err != nil {
		m.notice = err.Error()
		return
	}
	if m.session.ID == saved.ID {
		m.session = saved
	}
	d.renaming, m.notice, m.info = false, "", textFor("Conversa renomeada", "Conversation renamed")
	d.reload(m, saved.ID)
}

// preview is the right column: title, facts, the last exchange and actions.
func (d *sessionsDialog) preview(m *Model, item Item, width int) []string {
	s := item.Value.(session.Session)
	if _, ok := d.previews[s.ID]; !ok {
		d.previews[s.ID] = m.lastExchange(s.ID)
	}
	title := on(P.Surface, P.Strong).Bold(true).Render(ansi.Truncate(s.Title, width, "…"))
	if d.renaming {
		draft := ansi.TruncateLeft(d.draft, max(0, ansi.StringWidth(d.draft)-width+5), "…")
		title = Line(on(P.Field, P.Ask).Render(" ✎ ")+on(P.Field, P.Strong).Render(draft)+on(P.Field, P.Ask).Render("▏"), width, P.Field)
	}
	facts := []string{fmt.Sprintf(textFor("%d mensagens", "%d messages"), s.MessageCount)}
	if s.Cost > 0 {
		facts = append(facts, fmt.Sprintf("$%.2f", s.Cost))
	}
	if d.archived[s.ID] {
		facts = append(facts, textFor("arquivada", "archived"))
	}
	rows := []string{title, on(P.Surface, P.Muted).Render(ansi.Truncate(strings.Join(facts, " · "), width, "…")), ""}
	quote := func(text, bar string, barColor, fg color.Color, lines int) {
		wrapped := strings.Split(ansi.Wrap(strings.Join(strings.Fields(text), " "), width-2, ""), "\n")
		if len(wrapped) > lines {
			wrapped = append(wrapped[:lines-1], ansi.Truncate(wrapped[lines-1], width-3, "")+"…")
		}
		for _, line := range wrapped {
			rows = append(rows, on(P.Surface, barColor).Render(bar)+on(P.Surface, fg).Render(" "+line))
		}
		rows = append(rows, "")
	}
	if prompt := d.previews[s.ID][0]; prompt != "" {
		quote(prompt, "┃", P.User, P.Text, 3)
	}
	if reply := d.previews[s.ID][1]; reply != "" {
		quote(strings.NewReplacer("**", "", "`", "", "#", "").Replace(reply), "│", P.Ask, P.Muted, 4)
	}
	gap := on(P.Surface, P.Surface).Render(" ")
	if d.renaming {
		return append(rows, Button(textFor("Salvar · Enter", "Save · Enter"), true, P.Ask)+gap+Button(textFor("Cancelar · Esc", "Cancel · Esc"), false, P.Ask))
	}
	archive := textFor("Arquivar", "Archive")
	if d.archived[s.ID] {
		archive = textFor("Restaurar", "Restore")
	}
	return append(rows, Button(textFor("Abrir", "Open"), true, P.Ask)+gap+Button(textFor("Renomear", "Rename"), false, P.Ask)+gap+Button(textFor("Exportar", "Export"), false, P.Ask)+gap+Button(archive, false, P.Ask))
}

// lastExchange is the conversation's latest prompt from you and its reply.
func (m *Model) lastExchange(id string) [2]string {
	messages, err := m.app.Messages.List(context.Background(), id)
	if err != nil {
		return [2]string{}
	}
	var out [2]string
	for i := len(messages) - 1; i >= 0 && out[0] == ""; i-- {
		text := strings.TrimSpace(messages[i].Content().String())
		switch {
		case messages[i].Role == message.User && !messages[i].IsApplication():
			out[0] = text
		case messages[i].Role == message.Assistant && out[1] == "" && text != "":
			out[1] = text
		}
	}
	return out
}

func (m *Model) openSession(s session.Session) {
	m.dialog = nil
	m.session, m.notice, m.info = s, "", ""
	m.messages, _ = m.app.Messages.List(context.Background(), s.ID)
	m.busy = m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(s.ID)
	m.follow = true
	m.refresh()
}
