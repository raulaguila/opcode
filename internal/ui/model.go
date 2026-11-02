package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/workflow"
)

// Model is the conversation screen: header, transcript, composer and footer.
type Model struct {
	app      *app.App
	session  session.Session
	messages []message.Message

	width, height int
	transcript    viewport.Model
	composer      textarea.Model
	follow        bool // keep the transcript pinned to its end

	mode     string // ask, plan or execute
	busy     bool
	started  time.Time
	approval *permission.PermissionRequest
	policy   permission.ApprovalPolicy
	notice   string
	info     string   // a passing confirmation, shown in the footer
	dialog   floating // the panel floating over the screen, if any
	showPlan bool
	question *question
	task     workflow.Task

	failure     error  // why the last run stopped, until dismissed
	showFailure bool   // the raw reason, on request
	lastText    string // the last message sent, for resuming
}

type tickMsg struct{}

// New builds the screen; an initial session resumes that conversation.
func New(a *app.App, initial ...session.Session) *Model {
	if a != nil && a.Questions != nil {
		// This interface answers the agent's questions, so the agent may ask them.
		a.Questions.SetInteractive(true)
	}
	composer := textarea.New()
	composer.Prompt = ""
	composer.ShowLineNumbers = false
	composer.CharLimit = -1
	composer.SetHeight(1)
	styles := composer.Styles()
	for _, s := range []*textarea.StyleState{&styles.Focused, &styles.Blurred} {
		s.Base = on(P.Ground, P.Text)
		s.Text = on(P.Ground, P.Text)
		s.CursorLine = on(P.Ground, P.Text)
		s.Placeholder = on(P.Ground, P.Faint)
		s.EndOfBuffer = on(P.Ground, P.Ground)
	}
	composer.SetStyles(styles)
	composer.Focus()

	// Markdown reuses the shared renderer; its palette is the closest theme.
	// Preview only: the user's saved theme is never rewritten.
	_ = theme.PreviewTheme("tokyonight")
	m := &Model{app: a, composer: composer, transcript: viewport.New(), follow: true, mode: "execute"}
	if cfg := config.Get(); cfg != nil {
		m.mode = cfg.Runtime.Defaults().Mode
	}
	if len(initial) > 0 {
		m.session = initial[0]
		if a != nil && a.Messages != nil {
			m.messages, _ = a.Messages.List(context.Background(), m.session.ID)
		}
	}
	return m
}

func (m *Model) Init() tea.Cmd { return textarea.Blink }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.settingsMessage(msg); ok {
		return m, cmd
	}
	// A hosted settings dialog also hears its own async results.
	var hostedCmd tea.Cmd
	if h, ok := m.dialog.(*hosted); ok {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg, tea.MouseMsg, tea.WindowSizeMsg:
		default:
			hostedCmd = h.forward(msg)
		}
	}
	model, cmd := m.update(msg)
	return model, tea.Batch(hostedCmd, cmd)
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		if h, ok := m.dialog.(*hosted); ok {
			return m, h.forward(msg)
		}
		return m, nil
	case tea.MouseMsg:
		if _, ok := m.dialog.(*hosted); ok {
			return m, m.hostedMouse(msg)
		}
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && m.dialog == nil {
			return m, m.click(click.X, click.Y)
		}
		if wheel, ok := msg.(tea.MouseWheelMsg); ok {
			var cmd tea.Cmd
			m.transcript, cmd = m.transcript.Update(wheel)
			m.follow = m.transcript.AtBottom()
			return m, cmd
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.PasteMsg:
		// Pasted text, newlines included, lands in the draft; it never sends.
		if d, ok := m.dialog.(interface{ paste(string) }); ok {
			d.paste(strings.TrimSpace(msg.Content))
			return m, nil
		}
		m.composer.InsertString(msg.Content)
		m.layout()
		return m, nil
	case tickMsg:
		m.poll()
		// The final event can arrive before the agent marks the session free.
		if m.app != nil && m.app.CoderAgent != nil {
			m.busy = m.app.CoderAgent.IsSessionBusy(m.session.ID)
		}
		m.refresh()
		if m.busy {
			return m, tick()
		}
		return m, nil
	case catalogMsg:
		if picker, ok := m.dialog.(*modelPicker); ok {
			picker.accept(m, msg)
		}
		return m, nil
	case probeMsg:
		if panel, ok := m.dialog.(*settingsPanel); ok {
			panel.probes[msg.provider] = msg
		}
		return m, nil
	case pubsub.Event[message.Message]:
		m.upsert(msg)
	case pubsub.Event[session.Session]:
		if msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
		}
	case pubsub.Event[permission.PermissionRequest]:
		if msg.Payload.SessionID == m.session.ID {
			request := msg.Payload
			m.approval = &request
			m.layout()
		}
	case pubsub.Event[agent.AgentEvent]:
		event := msg.Payload
		if event.Type == agent.AgentEventTypeError || event.Done || event.Type == agent.AgentEventTypeResponse {
			m.busy = m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(m.session.ID)
			switch {
			case errors.Is(event.Error, agent.ErrRequestCancelled):
				m.info = textFor("Execução cancelada · rascunho preservado", "Run cancelled · draft kept")
			case event.Error != nil:
				m.failure = event.Error
			}
			m.poll()
			if !m.busy {
				return m, tick() // confirm the session settled
			}
		}
	}
	m.refresh()
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	if m.approval != nil {
		return m.answerApproval(msg.String())
	}
	if m.dialog != nil {
		cmd, _ := m.dialog.key(m, msg)
		return cmd
	}
	if m.question != nil && msg.String() != "ctrl+c" {
		return m.questionKey(msg)
	}
	if m.failure != nil {
		if cmd, ok := m.failureKey(msg.String()); ok {
			return cmd
		}
	}
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "ctrl+k":
		return m.openPalette()
	case "ctrl+o":
		return m.openModels()
	case "ctrl+s":
		return m.openSessions()
	case "ctrl+n":
		return m.newConversation()
	case "/":
		if strings.TrimSpace(m.composer.Value()) == "" {
			return m.openPalette()
		}
	case "esc":
		if m.busy && m.app.CoderAgent != nil {
			m.app.CoderAgent.Cancel(m.session.ID)
		}
		return nil
	case "shift+tab":
		m.cycleMode()
		return nil
	case "enter":
		return m.submit()
	case "alt+enter", "shift+enter", "ctrl+j":
		m.composer.InsertString("\n")
		m.layout()
		return nil
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.transcript, cmd = m.transcript.Update(msg)
		m.follow = m.transcript.AtBottom()
		return cmd
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	m.layout()
	return cmd
}

// answerApproval maps the numbered buttons (and a/s/d) to the decision.
func (m *Model) answerApproval(key string) tea.Cmd {
	request := *m.approval
	switch key {
	case "1", "a", "enter":
		m.app.Permissions.Grant(request)
	case "2", "s":
		m.app.Permissions.GrantPersistant(request)
	case "3", "d", "esc":
		m.app.Permissions.Deny(request)
	default:
		return nil
	}
	m.approval = nil
	m.layout()
	return nil
}

func (m *Model) cycleMode() {
	order := []string{"ask", "plan", "execute"}
	next := order[(slices.Index(order, m.mode)+1)%len(order)]
	if m.app == nil {
		m.mode = next
		return
	}
	m.setMode(next)
}

func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.composer.Value())
	if text == "" || m.app == nil || m.app.CoderAgent == nil {
		return nil
	}
	ctx := context.Background()
	if m.session.ID == "" {
		created, err := m.app.Sessions.Create(ctx, config.Text("Nova conversa", "New conversation"))
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		m.session = created
		_ = m.app.CoderAgent.SetMode(ctx, m.session.ID, workflow.Mode(m.mode))
		if m.policy != "" {
			m.setApproval(m.policy)
		}
	}
	if _, _, err := m.app.CoderAgent.Submit(ctx, m.session.ID, text); err != nil {
		m.notice = err.Error()
		return nil
	}
	m.composer.Reset()
	m.lastText, m.failure, m.info = text, nil, ""
	m.busy, m.started, m.notice, m.follow = true, time.Now(), "", true
	m.layout()
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// upsert keeps this conversation's messages in order as they stream.
func (m *Model) upsert(event pubsub.Event[message.Message]) {
	msg := event.Payload
	if msg.SessionID != m.session.ID || m.session.ID == "" {
		return
	}
	index := slices.IndexFunc(m.messages, func(existing message.Message) bool { return existing.ID == msg.ID })
	switch {
	case event.Type == pubsub.DeletedEvent && index >= 0:
		m.messages = slices.Delete(m.messages, index, index+1)
	case index >= 0:
		m.messages[index] = msg
	case event.Type != pubsub.DeletedEvent:
		m.messages = append(m.messages, msg)
	}
}

// layout sizes the transcript to what the header, composer, any approval
// and the footer leave.
func (m *Model) layout() {
	if m.width == 0 {
		return
	}
	lines := strings.Count(m.composer.Value(), "\n") + 1
	m.composer.SetWidth(max(1, m.width-8))
	m.composer.SetHeight(min(6, max(1, lines)))
	used := headerRows + m.composerRows() + footerRows
	if work := m.workView(); work != "" {
		used += strings.Count(work, "\n") + 1
	}
	m.transcript.SetWidth(m.width)
	m.transcript.SetHeight(max(1, m.height-used))
	m.refresh()
}

func (m *Model) refresh() {
	if m.width == 0 {
		return
	}
	// Every row is painted to the edge so no terminal background shows through.
	rows := strings.Split(m.renderTranscript(m.width), "\n")
	for i := range rows {
		rows[i] = Line(rows[i], m.width, P.Ground)
	}
	m.transcript.SetContent(strings.Join(rows, "\n"))
	if m.follow {
		m.transcript.GotoBottom()
	}
}

// click handles the footer controls and the mode's name on the composer.
func (m *Model) click(x, y int) tea.Cmd {
	switch y {
	case m.height - 1:
		if x < 3+ansi.StringWidth(textFor("Aprov.: editar no projeto ▾", "Approval: project edits ▾")) && x < m.width/3 {
			return m.openPaletteWith(textFor("Aprovação", "Approval"))
		}
		if m.app != nil && m.app.CoderAgent != nil && x >= m.width-2-ansi.StringWidth(modelName(m.app.CoderAgent.Model().ID)+" ▾") {
			return m.openModels()
		}
	case m.height - 1 - m.composerRows():
		if x >= 2 && x < 4+ansi.StringWidth(modeLabel(m.mode)) {
			m.cycleMode()
		}
	}
	return nil
}
