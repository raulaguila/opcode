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
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/fileutil"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/transcript"
	"github.com/raulaguila/opcode/internal/workflow"
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
	clearing     bool // the first Ctrl+L asked for confirmation
	acting       bool // → moved from the list to the preview's buttons
	action       int  // the button Enter presses while acting
	previews     map[string]preview
	asked        map[string]bool // previews being read
}

// preview is what the side column shows of a conversation.
type preview struct {
	prompt, reply, mode, model string
	planDone, planTotal        int
	calls                      []message.ToolCall
	results                    map[string]message.ToolResult
}

// openSessions lists conversations by day, the selected one previewed beside.
func (m *Model) openSessions() tea.Cmd {
	if m.app == nil || m.app.Sessions == nil {
		return nil
	}
	d := &sessionsDialog{previews: map[string]preview{}}
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
	return d.wanted(m)
}

func (d *sessionsDialog) catalog() *session.Catalog {
	cfg := config.Get()
	return session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory)
}

// reload reads the conversations again and keeps the selection on keep.
func (d *sessionsDialog) reload(m *Model, keep string) {
	d.legend = textFor("Enter abrir · → ações · Ctrl+R renomear · Ctrl+E exportar · Ctrl+A arquivar · Ctrl+H arquivadas · Ctrl+L limpar · Esc fechar", "Enter open · → actions · Ctrl+R rename · Ctrl+E export · Ctrl+A archive · Ctrl+L clear · Ctrl+H archived · Esc close")
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
		case m.failed[s.ID]:
			item.Mark, item.MarkColor = "■", P.Danger
		case planning(m, s.ID):
			item.Mark, item.MarkColor = "◐", P.Plan
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
	key := msg.String()
	if key == "ctrl+l" {
		return nil, d.clear(m)
	}
	d.clearing = false
	switch key {
	case "right":
		d.action = min(d.action+1, 3)
		if !d.acting {
			d.acting, d.action = true, 0
		}
		return nil, true
	case "left":
		d.acting, d.action = d.action > 0 && d.acting, max(0, d.action-1)
		return nil, true
	case "enter":
		if d.acting {
			key = []string{"enter", "ctrl+r", "ctrl+e", "ctrl+a"}[d.action]
			d.acting = false
			if key == "enter" {
				return nil, false // the list opens it
			}
		}
	default:
		d.acting = false
	}
	switch key {
	case "ctrl+h":
		d.showArchived = !d.showArchived
		d.reload(m, "")
		return nil, true
	case "ctrl+r", "ctrl+e", "ctrl+a":
	default:
		if key == "enter" {
			return nil, false
		}
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
	switch key {
	case "ctrl+r":
		d.renaming, d.draft = true, s.Title
	case "ctrl+e":
		cfg := config.Get()
		messages, err := m.app.Messages.List(context.Background(), s.ID)
		if err == nil {
			var path string
			if path, err = transcript.Save(cfg.Data.Directory, cfg.WorkingDir, s, messages, tools.RedactCredentials); err == nil {
				m.info = textFor("Conversa exportada: ", "Conversation exported: ") + fileutil.HomeRelative(path)
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

// clear deletes every conversation but the open and the archived ones, and
// the running ones; deleting cannot be undone, so it takes a second press.
func (d *sessionsDialog) clear(m *Model) bool {
	var doomed []session.Session
	for _, item := range d.list.Items {
		s := item.Value.(session.Session)
		if s.ID != m.session.ID && !d.archived[s.ID] && (m.app.CoderAgent == nil || !m.app.CoderAgent.IsSessionBusy(s.ID)) {
			doomed = append(doomed, s)
		}
	}
	switch {
	case len(doomed) == 0:
		m.info = textFor("Nenhuma conversa para limpar", "No conversations to clear")
	case !d.clearing:
		d.clearing = true
		m.notice = fmt.Sprintf(textFor("Ctrl+L de novo apaga %d conversas para sempre; arquivadas ficam. Outra tecla cancela", "Ctrl+L again deletes %d conversations for good; archived ones stay. Any other key cancels"), len(doomed))
	default:
		d.clearing, m.notice = false, ""
		for _, s := range doomed {
			if err := m.app.Sessions.Delete(context.Background(), s.ID); err != nil {
				m.notice = err.Error()
				break
			}
		}
		m.numbered = "" // the header's count changed
		m.info = fmt.Sprintf(textFor("%d conversas apagadas", "%d conversations deleted"), len(doomed))
		d.reload(m, m.session.ID)
	}
	return true
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
	title := on(P.Surface, P.Strong).Bold(true).Render(ansi.Truncate(s.Title, width, "…"))
	if d.renaming {
		draft := ansi.TruncateLeft(d.draft, max(0, ansi.StringWidth(d.draft)-width+5), "…")
		title = Line(on(P.Field, P.Ask).Render(" ✎ ")+on(P.Field, P.Strong).Render(draft)+on(P.Field, P.Ask).Render("▏"), width, P.Field)
	}
	look := d.previews[s.ID]
	// "Executar · nemotron · 14 mensagens · plano 2/5"
	var facts []string
	if look.mode != "" {
		facts = append(facts, modeLabel(look.mode))
	}
	if look.model != "" {
		facts = append(facts, look.model)
	}
	facts = append(facts, fmt.Sprintf(textFor("%d mensagens", "%d messages"), s.MessageCount))
	if look.planTotal > 0 {
		facts = append(facts, fmt.Sprintf(textFor("plano %d/%d", "plan %d/%d"), look.planDone, look.planTotal))
	}
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
	if look.prompt != "" {
		quote(look.prompt, "┃", P.User, P.Text, 3)
	}
	if len(look.calls) > 0 {
		steps := stepsRows(look.calls, look.results, nil, width, false, P.Surface)
		rows = append(rows, steps[max(0, len(steps)-3):]...)
		rows = append(rows, "")
	}
	if reply := look.reply; reply != "" {
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
	// The pressed-looking button is the one Enter presses: Open, unless → moved on.
	var buttons []string
	for i, label := range []string{textFor("Abrir", "Open"), textFor("Renomear", "Rename"), textFor("Exportar", "Export"), archive} {
		buttons = append(buttons, Button(label, (d.acting && i == d.action) || (!d.acting && i == 0), P.Ask))
	}
	return append(rows, strings.Join(buttons, gap))
}

// previewMsg brings a conversation's preview, read off the interface's loop.
type previewMsg struct {
	id   string
	look preview
}

// wanted reads the selected conversation's preview in the background the
// first time it is selected; until then the column shows what the list knows.
func (d *sessionsDialog) wanted(m *Model) tea.Cmd {
	item, ok := d.list.Current()
	if !ok || m.app == nil {
		return nil
	}
	id := item.Value.(session.Session).ID
	if _, seen := d.previews[id]; seen || d.asked[id] {
		return nil
	}
	if d.asked == nil {
		d.asked = map[string]bool{}
	}
	d.asked[id] = true
	a := m.app
	return func() tea.Msg { return previewMsg{id, lookInto(a, id)} }
}

// lookInto reads a conversation's last exchange, its steps, the model that
// answered and its plan.
func lookInto(a *app.App, id string) preview {
	var out preview
	messages, err := a.Messages.List(context.Background(), id)
	if err != nil {
		return out
	}
	out.results = map[string]message.ToolResult{}
	for i := len(messages) - 1; i >= 0 && out.prompt == ""; i-- {
		msg := messages[i]
		for _, result := range msg.ToolResults() {
			out.results[result.ToolCallID] = result
		}
		text := strings.TrimSpace(msg.Content().String())
		switch {
		case msg.Role == message.User && !msg.IsApplication():
			out.prompt = text
		case msg.Role == message.Assistant:
			if out.reply == "" && text != "" {
				out.reply = text
			}
			if out.model == "" && msg.Model != "" {
				out.model = modelName(msg.Model)
			}
			out.calls = append(msg.ToolCalls(), out.calls...)
		}
	}
	if task, err := a.Workflows.Get(context.Background(), id); err == nil {
		out.mode, out.planTotal = string(task.Mode), len(task.Steps)
		for _, step := range task.Steps {
			if step.Status == "completed" {
				out.planDone++
			}
		}
	}
	return out
}

// planning says whether a conversation's plan has steps left.
func planning(m *Model, id string) bool {
	if m.app == nil || m.app.Workflows == nil {
		return false
	}
	task, err := m.app.Workflows.Get(context.Background(), id)
	if err != nil {
		return false
	}
	for _, step := range task.Steps {
		if step.Status != "completed" {
			return true
		}
	}
	return false
}

// markLast makes this conversation the one --resume finds; only a sent message
// earns that, so opening one to read it changes nothing.
func (m *Model) markLast() {
	if cfg := config.Get(); cfg != nil && m.session.ID != "" {
		if err := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory).Activate(m.session.ID); err != nil {
			logging.Warn("Could not record the last conversation", "error", err)
		}
	}
}

func (m *Model) openSession(s session.Session) {
	m.dialog = nil
	m.leaveFullAccess()
	m.foldPlan, m.task = false, workflow.Task{}
	m.session, m.notice, m.info = s, "", ""
	// The footer's choice follows you; the service keeps it per conversation.
	if policies, ok := m.app.Permissions.(permission.ApprovalPolicies); ok && policies.ApprovalPolicy(s.ID) != m.policy {
		m.setApproval(m.policy)
	}
	m.messages, m.version = nil, m.version+1
	if messages, err := m.app.Messages.List(context.Background(), s.ID); err != nil {
		// An empty screen would look like a lost conversation.
		m.notice = textFor("Não foi possível ler a conversa: ", "Could not read the conversation: ") + err.Error()
	} else {
		m.messages = messages
	}
	m.busy = m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(s.ID)
	m.follow = true
	m.poll() // nothing else reads the plan of a conversation at rest
}
