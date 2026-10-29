package chat

import (
	"context"
	"fmt"
	"math"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
}
type messagesCmp struct {
	app             *app.App
	width, height   int
	viewport        viewport.Model
	session         session.Session
	messages        []message.Message
	uiMessages      []uiMessage
	currentMsgID    string
	cachedContent   map[string]cacheItem
	spinner         spinner.Model
	rendering       bool
	attachments     viewport.Model
	expanded        map[string]bool
	toggled         map[string]bool
	hits            []transcriptHit
	unread          bool
	lastWorking     bool
	transcriptLines []string
	selection       textSelection
}
type renderFinishedMsg struct{ sessionID string }

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

func (m *messagesCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case ProcessPreferencesChangedMsg:
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
		m.selection = textSelection{}
		m.transcriptLines = nil
		m.session = session.Session{}
		m.messages = make([]message.Message, 0)
		m.currentMsgID = ""
		m.rendering = false
		return m, nil

	case ToggleProcessMsg:
		groups := m.groups()
		if len(groups) > 0 {
			m.toggleProcess(groups[len(groups)-1].key)
		}
		return m, nil
	case JumpToLatestMsg:
		m.ClearSelection()
		m.unread = false
		m.viewport.GotoBottom()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "alt+end" {
			m.ClearSelection()
			m.unread = false
			m.viewport.GotoBottom()
			return m, nil
		}
		if msg.String() == "ctrl+b" {
			groups := m.groups()
			if len(groups) > 0 {
				m.toggleProcess(groups[len(groups)-1].key)
			}
			return m, nil
		}
		if msg.String() == "ctrl+y" {
			return m, util.CmdHandler(ResponseActionsMsg{m.LatestResponse()})
		}
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			u, cmd := m.viewport.Update(msg)
			m.viewport = u
			cmds = append(cmds, cmd)
		}
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft || msg.Action == tea.MouseActionRelease {
			return m, m.transcriptMouse(msg)
		}
		u, cmd := m.viewport.Update(msg)
		m.viewport = u
		cmds = append(cmds, cmd)

	case renderFinishedMsg:
		if msg.sessionID != m.session.ID {
			return m, nil
		}
		m.rendering = false
		m.viewport.GotoBottom()
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
			if m.session.SummaryMessageID == m.currentMsgID {
				delete(m.cachedContent, m.currentMsgID)
				m.renderView()
			}
		}
	case pubsub.Event[message.Message]:
		needsRerender := false
		if msg.Type == pubsub.CreatedEvent {
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
						delete(m.cachedContent, lastMsgID)
					}

					m.messages = append(m.messages, msg.Payload)
					delete(m.cachedContent, m.currentMsgID)
					m.currentMsgID = msg.Payload.ID
					needsRerender = true
				}
			}
			// There are tool calls from the child task
			for _, v := range m.messages {
				for _, c := range v.ToolCalls() {
					if c.ID == msg.Payload.SessionID {
						delete(m.cachedContent, v.ID)
						needsRerender = true
					}
				}
			}
		} else if msg.Type == pubsub.UpdatedEvent && msg.Payload.SessionID == m.session.ID {
			for i, v := range m.messages {
				if v.ID == msg.Payload.ID {
					m.messages[i] = msg.Payload
					delete(m.cachedContent, msg.Payload.ID)
					needsRerender = true
					break
				}
			}
		}
		if needsRerender {
			// Keep following only while the reader is at the bottom. Streaming
			// updates must not interrupt someone reading earlier messages.
			follow := !m.selection.engaged() && (m.viewport.AtBottom() || (msg.Type == pubsub.CreatedEvent &&
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

	if len(m.messages) > 0 && m.lastWorking != m.IsAgentWorking() {
		offset, follow := m.viewport.YOffset, m.viewport.AtBottom() && !m.selection.engaged()
		m.renderView()
		if follow {
			m.viewport.GotoBottom()
		} else {
			m.viewport.SetYOffset(offset)
		}
	}
	spinner, cmd := m.spinner.Update(msg)
	m.spinner = spinner
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *messagesCmp) IsAgentWorking() bool {
	return m.app != nil && m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(m.session.ID)
}

func formatTimeDifference(unixTime1, unixTime2 int64) string {
	diffSeconds := float64(math.Abs(float64(unixTime2 - unixTime1)))

	if diffSeconds < 60 {
		return fmt.Sprintf("%.1fs", diffSeconds)
	}

	minutes := int(diffSeconds / 60)
	seconds := int(diffSeconds) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func (m *messagesCmp) renderView() { m.renderTurns() }

func (m *messagesCmp) View() string {
	content := m.highlightSelection(m.viewport.View())
	if m.rendering {
		content = config.Text("Carregando conversa…", "Loading conversation…")
	} else if len(m.messages) == 0 {
		content = m.initialScreen()
	}
	if m.unread {
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
	for _, msg := range m.messages {
		delete(m.cachedContent, msg.ID)
	}
	m.renderView()
}

func (m *messagesCmp) SetSize(width, height int) tea.Cmd {
	if m.width == width && m.height == height {
		return nil
	}
	m.selection = textSelection{} // Reflow invalidates display-cell coordinates.
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = max(0, height-1)
	m.attachments.Width = width + 40
	m.attachments.Height = 3
	m.rerender()
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
	m.session = session
	messages, err := m.app.Messages.List(context.Background(), session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.messages = messages
	if len(m.messages) > 0 {
		m.currentMsgID = m.messages[len(m.messages)-1].ID
	}
	delete(m.cachedContent, m.currentMsgID)
	m.rendering = true
	// Model mutations belong to the event loop, not to asynchronous commands.
	m.renderView()
	return func() tea.Msg {
		return renderFinishedMsg{sessionID: session.ID}
	}
}

func (m *messagesCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		m.viewport.KeyMap.PageDown,
		m.viewport.KeyMap.PageUp,
		m.viewport.KeyMap.HalfPageUp,
		m.viewport.KeyMap.HalfPageDown,
	}
}

func NewMessagesCmp(app *app.App) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Pulse
	vp := viewport.New(0, 0)
	attachmets := viewport.New(0, 0)
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
