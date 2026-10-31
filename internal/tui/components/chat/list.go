package chat

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type cacheItem struct {
	width   int
	content []uiMessage
	bytes   int
	used    uint64
}
type messagesCmp struct {
	app             *app.App
	width, height   int
	viewport        transcriptViewport
	session         session.Session
	messages        []message.Message
	uiMessages      []uiMessage
	currentMsgID    string
	cachedContent   map[string]cacheItem
	renderCache     transcriptCache
	nested          nestedHistory
	reconcile       messageReconciliation
	history         transcriptHistory
	spinner         spinner.Model
	rendering       bool
	loadingPreview  loadingPreview
	attachments     viewport.Model
	expanded        map[string]bool
	toggled         map[string]bool
	hits            []transcriptHit
	unread          bool
	lastWorking     bool
	transcriptLines []string
	selection       textSelection
	find            transcriptFind
}

type MessageKeys struct {
	PageDown     key.Binding
	PageUp       key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
}

var messageKeys = MessageKeys{
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdn", "page down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "page up"),
	),
	HalfPageUp: key.NewBinding(
		key.WithKeys("alt+pgup"),
		key.WithHelp("alt+pgup", "½ page up"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("alt+pgdown"),
		key.WithHelp("alt+pgdn", "½ page down"),
	),
}

func (m *messagesCmp) Init() tea.Cmd {
	return tea.Batch(m.viewport.Init(), m.spinner.Tick)
}

func (m *messagesCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case messageReloadMsg:
		return m, m.acceptMessageReload(msg)
	case nestedHistoryMsg:
		return m, m.acceptNestedHistory(msg)
	case ProcessPreferencesChangedMsg:
		m.renderCache.dirty = true
		m.renderView()
		return m, nil
	case dialog.ThemeChangedMsg:
		m.rerender()
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			cmd := m.SetSession(msg)
			return m, cmd
		}
		return m, nil
	case SessionClearedMsg:
		m.resetRenderCache()
		m.resetNestedHistory()
		m.resetMessageReconciliation()
		m.find = transcriptFind{}
		m.selection = textSelection{}
		m.transcriptLines = nil
		m.viewport.SetLines(nil)
		m.resetTranscriptHistory()
		m.session = session.Session{}
		m.messages = make([]message.Message, 0)
		m.currentMsgID = ""
		m.rendering = false
		return m, nil

	case ToggleProcessMsg:
		for i := len(m.hits) - 1; i >= 0; i-- {
			if m.hits[i].kind == "process" {
				m.toggleProcess(m.hits[i].id)
				break
			}
		}
		return m, nil
	case JumpToLatestMsg:
		m.ClearSelection()
		m.unread = false
		m.viewport.GotoBottom()
		return m, nil
	case tea.KeyPressMsg:
		if result0, result1, handled := updateMessagesCmpKeyMsg(m, &cmds, &msg); handled {
			return result0, result1
		}
	case tea.MouseMsg:
		if msg.Mouse().Button == tea.MouseLeft || util.MouseReleased(msg) {
			cmd := m.transcriptMouse(msg)
			return m, tea.Batch(cmd, m.HistoryCommand())
		}
		cmds = append(cmds, m.updateTranscriptViewport(msg))

	case pubsub.Event[session.Session]:
		updateEventSessionSession(m, &msg)
	case pubsub.Event[message.Message]:
		reload, nested := m.updateMessageEvent(msg)
		cmds = append(cmds, reload, nested)
	}

	updateMessagesLastWorkingIsAgentWorking(m)
	spinner, cmd := m.spinner.Update(msg)
	m.spinner = spinner
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *messagesCmp) IsAgentWorking() bool {
	return m.app != nil && m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(m.session.ID)
}

func (m *messagesCmp) renderView() { m.renderTurns() }

func (m *messagesCmp) View() string {
	content := m.highlightSelection(m.viewport.View())
	if m.rendering {
		content = m.loadingView()
	} else if len(m.messages) == 0 {
		content = m.initialScreen()
	}
	if m.find.open || m.selection.keyboard {
		content = lipgloss.JoinVertical(lipgloss.Left, content, m.navigationHint())
	} else if m.unread {
		content = lipgloss.JoinVertical(lipgloss.Left, content, m.working())
	}
	return styles.BaseStyle().Width(m.width).Height(max(0, m.height)).MaxHeight(max(0, m.height)).Render(content)
}

func (m *messagesCmp) working() string {
	if m.unread {
		return styles.BaseStyle().Foreground(theme.CurrentTheme().Primary()).Width(m.width).Render(config.Text(" Novas mensagens ↓ · Alt+End acompanha", " New messages ↓ · Alt+End to follow"))
	}
	return ""
}

func (m *messagesCmp) initialScreen() string {
	return styles.BaseStyle().Foreground(theme.CurrentTheme().Text()).Width(m.width).Render(config.Text("O que vamos fazer neste projeto?", "What would you like to do in this project?"))
}

func (m *messagesCmp) rerender() {
	m.cachedContent = make(map[string]cacheItem)
	m.renderCache = transcriptCache{dirty: true}
	m.find.normalized = nil
	m.find.normalizedBytes = 0
	m.renderView()
}

func (m *messagesCmp) SetSize(width, height int) tea.Cmd {
	if m.width == width && m.height == height {
		return nil
	}
	follow := m.viewport.AtBottom() && !m.selection.engaged() && !m.find.open
	m.selection = textSelection{} // Reflow invalidates display-cell coordinates.
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = max(0, height-1)
	m.attachments.SetWidth(width + 40)
	m.attachments.SetHeight(3)
	m.rerender()
	if follow {
		m.viewport.GotoBottom()
	}
	return nil
}

func (m *messagesCmp) GetSize() (int, int) {
	return m.width, m.height
}

func (m *messagesCmp) SetSession(session session.Session) tea.Cmd {
	if m.session.ID == session.ID {
		return nil
	}
	m.selection = textSelection{}
	m.resetRenderCache()
	m.resetNestedHistory()
	m.resetMessageReconciliation()
	m.session = session
	m.find = transcriptFind{}
	m.messages = nil
	m.currentMsgID = ""
	m.viewport.SetLines(nil)
	m.resetTranscriptHistory()
	m.history.initial = true
	m.rendering = true
	m.reconcile.needed = true
	cmd := m.requestMessageReload()
	if cmd == nil {
		m.rendering = false
	}
	return cmd
}

func (m *messagesCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		m.viewport.KeyMap.PageDown,
		m.viewport.KeyMap.PageUp,
		m.viewport.KeyMap.HalfPageUp,
		m.viewport.KeyMap.HalfPageDown,
	}
}

func NewMessagesCmp(app *app.App) util.Model {
	s := spinner.New()
	s.Spinner = spinner.Pulse
	vp := newTranscriptViewport(0, 0)
	attachmets := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	vp.KeyMap.PageUp = messageKeys.PageUp
	vp.KeyMap.PageDown = messageKeys.PageDown
	vp.KeyMap.HalfPageUp = messageKeys.HalfPageUp
	vp.KeyMap.HalfPageDown = messageKeys.HalfPageDown
	return &messagesCmp{
		app:           app,
		cachedContent: make(map[string]cacheItem),
		viewport:      vp,
		spinner:       s,
		attachments:   attachmets,
	}
}

func (m *messagesCmp) updateMessageEvent(event pubsub.Event[message.Message]) (tea.Cmd, tea.Cmd) {
	event.Payload = message.HistorySnapshot(event.Payload)
	m.journalMessageEvent(event)
	if m.rendering {
		m.updateLoadingPreview(event)
	} else {
		updateMessagesCmpMessage(m, &event)
	}
	return m.reconcileMessageEvent(event), m.nestedMessageEvent(event)
}

func updateMessagesCmpMessage(m *messagesCmp, msg *pubsub.Event[message.Message]) {
	needsRerender := false
	if msg.Payload.SessionID == m.session.ID {
		m.invalidateToolResponses(msg.Payload)
		if msg.Type == pubsub.DeletedEvent {
			before := len(m.messages)
			m.messages = applyHistoryEvent(m.messages, *msg)
			m.invalidateMessageCache(msg.Payload.ID)
			needsRerender = before != len(m.messages)
		}
	}
	updateMessagesCmpMessageTypePubsubCreatedEvent(m, msg, &needsRerender)
	if needsRerender {
		// Keep following only while the reader is at the bottom. Streaming
		// updates must not interrupt someone reading earlier messages.
		follow := !m.selection.engaged() && !m.find.open && (m.viewport.AtBottom() || (msg.Type == pubsub.CreatedEvent &&
			msg.Payload.SessionID == m.session.ID && msg.Payload.Role == message.User))
		m.renderView()
		if follow {
			m.viewport.GotoBottom()
			m.unread = false
		} else {
			m.unread = true
		}
	}
}

func updateMessagesCmpKeyMsg(m *messagesCmp, cmds *[]tea.Cmd, msg *tea.KeyPressMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if m.TranscriptKey((*msg)) {
		return m, m.HistoryCommand(), true
	}
	if msg.String() == "alt+end" {
		m.ClearSelection()
		m.unread = false
		m.viewport.GotoBottom()
		return m, nil, true
	}
	if msg.String() == "ctrl+b" {
		groups := m.groups()
		if len(groups) > 0 {
			m.toggleProcess(groups[len(groups)-1].key)
		}
		return m, nil, true
	}
	if msg.String() == "ctrl+y" {
		return m, util.CmdHandler(ResponseActionsMsg{m.LatestResponse()}), true
	}
	if key.Matches((*msg), messageKeys.PageUp) || key.Matches((*msg), messageKeys.PageDown) ||
		key.Matches((*msg), messageKeys.HalfPageUp) || key.Matches((*msg), messageKeys.HalfPageDown) {
		(*cmds) = append((*cmds), m.updateTranscriptViewport(*msg))
	}
	return extractedResult0, extractedResult1, false
}

func updateMessagesCmpMessageTypePubsubCreatedEvent(m *messagesCmp, msg *pubsub.Event[message.Message], needsRerender *bool) {
	if msg.Type == pubsub.CreatedEvent {
		updateMessagesCmpMessageTypePubsubCreatedEventSessionIDSessionID(m, msg, needsRerender)
		// There are tool calls from the child task
		updateMessagesCmpMessageTypePubsubCreatedEventMessages(m, msg, needsRerender)
	} else if msg.Type == pubsub.UpdatedEvent && msg.Payload.SessionID == m.session.ID {
		for i, v := range m.messages {
			if v.ID == msg.Payload.ID {
				m.messages[i] = msg.Payload
				m.invalidateMessageCache(msg.Payload.ID)
				(*needsRerender) = true
				break
			}
		}
	}
}

func updateMessagesCmpMessageTypePubsubCreatedEventSessionIDSessionID(m *messagesCmp, msg *pubsub.Event[message.Message], needsRerender *bool) {
	if msg.Payload.SessionID == m.session.ID {

		messageExists := false
		for _, v := range m.messages {
			if v.ID == msg.Payload.ID {
				messageExists = true
				break
			}
		}

		if !messageExists {
			if len(m.messages) > 0 {
				lastMsgID := m.messages[len(m.messages)-1].ID
				m.invalidateMessageCache(lastMsgID)
			}

			m.messages = append(m.messages, msg.Payload)
			m.invalidateMessageCache(m.currentMsgID)
			m.currentMsgID = msg.Payload.ID
			(*needsRerender) = true
		}
	}
}

func updateMessagesLastWorkingIsAgentWorking(m *messagesCmp) {
	if len(m.messages) > 0 && m.lastWorking != m.IsAgentWorking() {
		offset, follow := m.viewport.YOffset, m.viewport.AtBottom() && !m.selection.engaged() && !m.find.open
		m.renderView()
		if follow {
			m.viewport.GotoBottom()
		} else {
			m.viewport.SetYOffset(offset)
		}
	}
}

func updateEventSessionSession(m *messagesCmp, msg *pubsub.Event[session.Session]) {
	if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
		m.session = msg.Payload
		if m.session.SummaryMessageID == m.currentMsgID {
			m.invalidateMessageCache(m.currentMsgID)
			m.renderView()
		}
	}
}

func updateMessagesCmpMessageTypePubsubCreatedEventMessages(m *messagesCmp, msg *pubsub.Event[message.Message], needsRerender *bool) {
	for _, v := range m.messages {
		for _, c := range v.ToolCalls() {
			if c.ID == msg.Payload.SessionID {
				m.invalidateMessageCache(v.ID)
				(*needsRerender) = true
			}
		}
	}
}
