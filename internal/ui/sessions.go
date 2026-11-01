package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
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

// openSessions lists conversations by day, the selected one previewed beside.
func (m *Model) openSessions() tea.Cmd {
	if m.app == nil || m.app.Sessions == nil {
		return nil
	}
	list, err := m.app.Sessions.List(context.Background())
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	slices.SortFunc(list, func(a, b session.Session) int { return int(b.UpdatedAt - a.UpdatedAt) })
	now := time.Now()
	items := make([]Item, 0, len(list))
	selected := 0
	for _, s := range list {
		if s.ParentSessionID != "" {
			continue
		}
		item := Item{ID: s.ID, Title: s.Title, Right: recency(s.UpdatedAt, now), Group: sessionDay(s.UpdatedAt, now), Value: s}
		if s.ID == m.session.ID {
			item.Mark = "●"
			selected = len(items)
		}
		items = append(items, item)
	}
	previews := map[string]string{}
	m.dialog = &listDialog{
		title:        textFor("Conversas", "Conversations"),
		legend:       textFor("↑↓ navegar · Enter abrir · Esc fechar", "↑↓ navigate · Enter open · Esc close"),
		searchPrompt: "⌕",
		width:        108,
		list:         List{Items: items, Grouped: true, Selected: selected},
		meta: func(d *listDialog) string {
			if n := len(d.list.Visible()); n != 1 {
				return fmt.Sprintf(textFor("%d conversas", "%d conversations"), n)
			}
			return textFor("1 conversa", "1 conversation")
		},
		side: func(m *Model, item Item, width int) []string {
			s := item.Value.(session.Session)
			if _, ok := previews[s.ID]; !ok {
				previews[s.ID] = m.lastPrompt(s.ID)
			}
			messages := fmt.Sprintf(textFor("%d mensagens", "%d messages"), s.MessageCount)
			rows := []string{
				on(P.Surface, P.Strong).Bold(true).Render(ansi.Truncate(s.Title, width, "…")),
				on(P.Surface, P.Muted).Render(ansi.Truncate(messages+" · "+recency(s.UpdatedAt, time.Now()), width, "…")),
				"",
			}
			if preview := previews[s.ID]; preview != "" {
				for _, line := range strings.Split(ansi.Wrap(preview, width-2, ""), "\n") {
					rows = append(rows, on(P.Surface, P.User).Render("┃")+on(P.Surface, P.Text).Render(" "+line))
				}
				rows = append(rows, "")
			}
			return append(rows, Button(textFor("Abrir · Enter", "Open · Enter"), true, P.Ask))
		},
		choose: func(m *Model, item Item) tea.Cmd {
			m.openSession(item.Value.(session.Session))
			return nil
		},
	}
	return nil
}

// lastPrompt is the conversation's latest message from you.
func (m *Model) lastPrompt(id string) string {
	messages, err := m.app.Messages.List(context.Background(), id)
	if err != nil {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == message.User && !messages[i].IsApplication() {
			return strings.TrimSpace(messages[i].Content().String())
		}
	}
	return ""
}

func (m *Model) openSession(s session.Session) {
	m.dialog = nil
	m.session, m.notice, m.info = s, "", ""
	m.messages, _ = m.app.Messages.List(context.Background(), s.ID)
	m.busy = m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(s.ID)
	m.follow = true
	m.refresh()
}
