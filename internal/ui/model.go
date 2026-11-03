package ui

import (
	"cmp"
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
	"github.com/raulaguila/opcode/internal/tui/styles"
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
	shiftEnter    bool // the terminal tells Shift+Enter from Enter

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
	recall      int    // how many sent messages back ↑ has gone; 0 is the draft
	draft       string // what was typed before ↑ recalled a message

	numbered string // the session whose number is counted
	number   int

	failed        map[string]bool // conversations whose last run stopped on an error
	fullDiff      bool            // the approval shows its whole diff (T)
	laterQuestion string          // a question put away to answer later (Esc)
	recent        []string        // palette commands used last, this run

	blocks       map[string]renderedBlock // messages already drawn, by ID
	live         styles.MarkdownCache     // the streaming reply's settled blocks
	ticking      bool                     // a clock tick is scheduled
	version      int                      // bumped whenever messages change
	paintKey     paintKey                 // what the painted transcript was drawn from
	rows         []string                 // its rows, painted to the width
	blank        []string                 // empty lines, as many as rows, for the viewport
	activity     string                   // what the run is doing, for the footer
	behind       string                   // the screen under the open panel
	dimmed       string                   // and the same, receded
	painted      map[string]string        // transcript rows padded to the width, by row
	paintedWidth int

	mention     []string  // files matching the @ being typed
	mentionPick int       // the one Tab or Enter inserts
	files       []string  // the workspace's files, for @
	filesAt     time.Time // when files was listed
	listing     bool      // a listing is on its way
	found       []string  // a search's matches, when the listing stopped short
	foundFor    string    // the query they match
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
			var err error
			if m.messages, err = a.Messages.List(context.Background(), m.session.ID); err != nil {
				m.notice = textFor("Não foi possível ler a conversa: ", "Could not read the conversation: ") + err.Error()
			}
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
	case tea.KeyboardEnhancementsMsg:
		m.shiftEnter = msg.Flags != 0
		return m, nil
	case tea.WindowSizeMsg:
		first := m.width == 0
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		if h, ok := m.dialog.(*hosted); ok {
			return m, h.forward(msg)
		}
		// With no model yet, the first screen is setting up a connection.
		if first && m.dialog == nil && !m.hasModel() {
			return m, m.openProviderForm("")
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
		cmd := m.key(msg)
		if d, ok := m.dialog.(*sessionsDialog); ok {
			cmd = tea.Batch(cmd, d.wanted(m))
		}
		return m, cmd
	case previewMsg:
		if d, ok := m.dialog.(*sessionsDialog); ok {
			d.previews[msg.id] = msg.look
		}
		return m, nil
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
		m.ticking = false
		m.poll()
		// The final event can arrive before the agent marks the session free.
		if m.app != nil && m.app.CoderAgent != nil {
			m.busy = m.app.CoderAgent.IsSessionBusy(m.session.ID)
		}
		if m.busy {
			m.refresh() // a running step counts its seconds
			return m, m.tick()
		}
		return m, nil
	case filesMsg:
		m.files, m.filesAt, m.listing = msg, time.Now(), false
		m.found, m.foundFor = nil, "" // searched against the older listing
		m.findMention()
		m.layout()
		return m, nil
	case searchMsg:
		return m, m.search(string(msg))
	case reviewedMsg:
		m.reviewed(msg)
	case foundMsg:
		if current, ok := m.mentionToken(); ok && strings.Contains(strings.ToLower(current), strings.ToLower(msg.query)) {
			m.found, m.foundFor = msg.files, msg.query
			m.findMention()
			m.layout()
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
			if id := cmp.Or(event.SessionID, event.Message.SessionID, m.session.ID); event.Error != nil && !errors.Is(event.Error, agent.ErrRequestCancelled) {
				if m.failed == nil {
					m.failed = map[string]bool{}
				}
				m.failed[id] = true
			}
			m.poll()
			if !m.busy {
				return m, m.tick() // confirm the session settled
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
	if m.question != nil && !m.answeringLater() && msg.String() != "ctrl+c" {
		return m.questionKey(msg)
	}
	if m.answeringLater() && msg.String() == "enter" && strings.TrimSpace(m.composer.Value()) == "" {
		m.laterQuestion = "" // the empty composer's Enter brings the question back
		m.layout()
		return nil
	}
	if m.failure != nil {
		if cmd, ok := m.failureKey(msg.String()); ok {
			return cmd
		}
	}
	if len(m.mention) > 0 && m.mentionKey(msg.String()) {
		return nil
	}
	if m.recallKey(msg.String()) {
		return nil
	}
	switch msg.String() {
	case "ctrl+c":
		m.dialog = confirmQuit{}
		return nil
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
	case "alt+up", "alt+down":
		if msg.String() == "alt+up" {
			m.transcript.ScrollUp(3)
		} else {
			m.transcript.ScrollDown(3)
		}
		m.follow = m.transcript.AtBottom()
		return nil
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.transcript, cmd = m.transcript.Update(msg)
		m.follow = m.transcript.AtBottom()
		return cmd
	}
	var cmd tea.Cmd
	before := m.composer.Value()
	m.composer, cmd = m.composer.Update(msg)
	if m.composer.Value() != before {
		m.recall = 0 // an edited message is a new draft
	}
	listing := m.findMention()
	m.layout()
	return tea.Batch(cmd, listing)
}

// answerApproval maps the numbered buttons (and a/s/d) to the decision.
func (m *Model) answerApproval(key string) tea.Cmd {
	request := *m.approval
	switch key {
	case "t", "T":
		m.fullDiff = !m.fullDiff
		m.layout()
		return nil
	case "1", "a", "enter":
		m.app.Permissions.Grant(request)
	case "2", "s":
		m.app.Permissions.GrantPersistant(request)
	case "3", "d", "esc":
		m.app.Permissions.Deny(request)
	default:
		return nil
	}
	m.approval, m.fullDiff = nil, false
	m.layout()
	return nil
}

// answeringLater is a pending question put away with Esc.
func (m *Model) answeringLater() bool {
	return m.question != nil && m.laterQuestion == m.question.q.ID
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
		if err := m.app.CoderAgent.SetMode(ctx, m.session.ID, workflow.Mode(m.mode)); err != nil {
			m.notice = err.Error() // the conversation exists; the message waits in the composer
			return nil
		}
		if m.policy != "" {
			m.setApproval(m.policy)
		}
	}
	if _, _, err := m.app.CoderAgent.Submit(ctx, m.session.ID, text); err != nil {
		m.notice = err.Error()
		return nil
	}
	m.composer.Reset()
	m.recall, m.draft = 0, ""
	m.lastText, m.failure, m.info = text, nil, ""
	delete(m.failed, m.session.ID)
	m.busy, m.started, m.notice, m.follow = true, time.Now(), "", true
	m.layout()
	return m.tick()
}

// tick schedules the next clock update, one at a time, on the second the
// run's elapsed time turns, so the footer counts every second.
func (m *Model) tick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	delay := time.Second - time.Since(m.started)%time.Second
	return tea.Tick(delay, func(time.Time) tea.Msg { return tickMsg{} })
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
		m.version++
	case index >= 0:
		m.messages[index] = msg
		m.version++
	case event.Type != pubsub.DeletedEvent:
		m.messages = append(m.messages, msg)
		m.version++
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
	used := headerRows + m.composerRows() + len(m.mention) + footerRows
	if work := m.workView(); work != "" {
		used += strings.Count(work, "\n") + 1
	}
	m.transcript.SetWidth(m.width)
	m.transcript.SetHeight(max(1, m.height-used))
	m.refresh()
}

// paintKey is everything the transcript is drawn from besides its width.
type paintKey struct {
	version, count, width, prose int
	head                         *message.Message
	failure                      error
	showFailure, waiting, model  bool
	process                      bool
}

func (m *Model) refresh() {
	if m.width == 0 {
		return
	}
	// Every row is painted to the edge so no terminal background shows
	// through; rows painted last time are reused.
	// Typing and resizing the composer change nothing the transcript shows,
	// so it is built again only when one of those things changed.
	key := paintKey{version: m.version, count: len(m.messages), width: m.width, failure: m.failure, showFailure: m.showFailure, waiting: m.approval != nil, model: m.hasModel()}
	if len(m.messages) > 0 {
		key.head = &m.messages[0] // a list replaced wholesale, even without a version bump
	}
	if cfg := config.Get(); cfg != nil {
		key.prose, key.process = cfg.TUI.ProseWidth, cfg.Preferences.ProcessVisible()
	}
	if key == m.paintKey && m.transcript.Width() == m.width {
		if m.follow {
			m.transcript.GotoBottom()
		}
		return
	}
	m.paintKey = key
	m.activity = m.doing() // read by every frame's footer; messages change only here
	content := m.renderTranscript(m.width)
	rows := strings.Split(content, "\n")
	painted := make(map[string]string, len(rows))
	for i, row := range rows {
		line, ok := m.painted[row]
		if !ok {
			line = Line(row, m.width, P.Ground)
		}
		painted[row], rows[i] = line, line
	}
	if m.paintedWidth == m.width {
		m.painted = painted
	} else {
		m.painted, m.paintedWidth = nil, m.width
	}
	m.rows = rows
	// The viewport keeps only the scroll position: transcriptRows cuts the
	// painted rows itself. Blank lines give it the count without making it
	// measure every row's width on each update.
	for len(m.blank) < len(rows) {
		m.blank = append(m.blank, "")
	}
	m.transcript.SetContentLines(m.blank[:len(rows)])
	if m.follow {
		m.transcript.GotoBottom()
	}
}

// click handles the footer controls and the mode's name on the composer.
func (m *Model) click(x, y int) tea.Cmd {
	switch y {
	case m.height - 1:
		x -= ansi.StringWidth(m.footerMode())
		if x < 3+ansi.StringWidth(textFor("Aprov.: editar no projeto ▾", "Approval: project edits ▾")) && x < m.width/3 {
			return m.openPaletteWith(textFor("Aprovação", "Approval"))
		}
		if m.app != nil && m.app.CoderAgent != nil && x >= m.width-2-ansi.StringWidth(modelName(m.app.CoderAgent.Model().ID)+" ▾") {
			return m.openModels()
		}
	case m.height - footerRows - m.composerRows():
		if x >= 2 && x < 4+ansi.StringWidth(modeLabel(m.mode)) {
			m.cycleMode()
		}
	}
	return nil
}

// hasModel says whether a model is chosen for the coder.
func (m *Model) hasModel() bool {
	return m.app != nil && m.app.CoderAgent != nil && m.app.CoderAgent.Model().ID != ""
}

// sent is what you wrote in this conversation, oldest first, without repeats
// in a row.
func (m *Model) sent() []string {
	var out []string
	for _, msg := range m.messages {
		if msg.Role != message.User || msg.IsApplication() {
			continue
		}
		if text := strings.TrimSpace(msg.Content().Text); text != "" && (len(out) == 0 || out[len(out)-1] != text) {
			out = append(out, text)
		}
	}
	return out
}

// recallKey walks your sent messages as a shell walks its history: ↑ on the
// composer's first line brings the one before, ↓ on its last line the one
// after, and past the newest the draft comes back.
func (m *Model) recallKey(key string) bool {
	sent := m.sent()
	m.recall = min(m.recall, len(sent)) // another conversation may have fewer
	switch {
	case key == "up" && m.composer.Line() == 0 && m.recall < len(sent):
		if m.recall == 0 {
			m.draft = m.composer.Value()
		}
		m.recall++
		m.composer.SetValue(sent[len(sent)-m.recall])
	case key == "down" && m.recall > 0 && m.composer.Line() == m.composer.LineCount()-1:
		m.recall--
		if m.recall == 0 {
			m.composer.SetValue(m.draft)
		} else {
			m.composer.SetValue(sent[len(sent)-m.recall])
		}
	default:
		return false
	}
	m.layout()
	return true
}
