package chat

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type ResponseActionsMsg struct{ Text string }
type ToggleProcessMsg struct{}
type ProcessPreferencesChangedMsg struct{}
type JumpToLatestMsg struct{}
type transcriptHit struct {
	start, end     int
	kind, id, text string
}
type TranscriptControl interface{ LatestResponse() string }
type turnGroup struct {
	user     *message.Message
	messages []message.Message
	key      string
}

func (m *messagesCmp) LatestResponse() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		v := m.messages[i]
		if v.Role == message.Assistant && len(v.ToolCalls()) == 0 && v.Content().String() != "" {
			return v.Content().String()
		}
	}
	return ""
}
func (m *messagesCmp) groups() []turnGroup {
	var groups []turnGroup
	for _, v := range m.messages {
		if v.Role == message.User && !v.IsApplication() {
			copy := v
			groups = append(groups, turnGroup{user: &copy, key: v.ID})
			continue
		}
		if len(groups) == 0 {
			groups = append(groups, turnGroup{key: v.ID})
		}
		groups[len(groups)-1].messages = append(groups[len(groups)-1].messages, v)
	}
	return groups
}
func (m *messagesCmp) renderTurns() {
	if m.width < 1 {
		return
	}
	groups := m.groups()
	m.lastWorking = len(m.messages) > 0 && m.IsAgentWorking()
	m.uiMessages = nil
	m.hits = nil
	base := styles.BaseStyle()
	t := theme.CurrentTheme()
	var rendered []string
	pos := 0
	appendItem := func(item uiMessage) {
		item.position = pos
		item.height = lipgloss.Height(item.content)
		m.uiMessages = append(m.uiMessages, item)
		rendered = append(rendered, item.content, "")
		pos += item.height + 1
	}
	appendText := func(id, text string, kind uiMessageType) {
		appendItem(uiMessage{ID: id, messageType: kind, content: base.Width(m.width).Render(text)})
	}
	for gi, g := range groups {
		if g.user != nil {
			cached, ok := m.cachedContent[g.user.ID]
			if !ok || cached.width != m.width {
				cached = cacheItem{width: m.width, content: []uiMessage{renderUserMessage(*g.user, g.key == m.currentMsgID, m.width, pos)}}
				m.cachedContent[g.user.ID] = cached
			}
			for _, item := range cached.content {
				appendItem(item)
			}
		}
		var finals []int
		actions := 0
		for i, v := range g.messages {
			actions += len(v.ToolCalls())
			if v.IsApplication() {
				actions++
				finals = nil
			}
			if v.Role == message.Assistant && len(v.ToolCalls()) == 0 && v.Content().String() != "" {
				finals = append(finals, i)
			}
		}
		hasProcess := actions > 0
		for _, v := range g.messages {
			if v.Role == message.Assistant && v.ReasoningContent().Thinking != "" {
				hasProcess = true
			}
		}
		if hasProcess {
			active := gi == len(groups)-1 && m.IsAgentWorking()
			expanded := false
			if cfg := config.Get(); cfg != nil {
				expanded = active && cfg.Preferences.ProcessVisible()
			}
			if m.toggled[g.key] {
				expanded = m.expanded[g.key]
			}
			mark, state := "›", "concluído"
			if active {
				state = "em andamento"
			}
			if expanded {
				mark = "⌄"
			}
			label := fmt.Sprintf(config.Text("%s Processo · %d ações", "%s Process · %d actions"), mark, actions)
			if active {
				label += " · " + config.Text(state, "working")
			}
			start := pos
			appendText(g.key+":process", base.Foreground(t.TextMuted()).Render(ansi.Truncate(label, m.width, "…")), toolMessageType)
			m.hits = append(m.hits, transcriptHit{start, start + 1, "process", g.key, ""})
			if expanded {
				for i, v := range g.messages {
					if v.IsApplication() {
						appendText(v.ID, base.Foreground(t.TextMuted()).Render(config.Text("Validação da aplicação", "Application validation")+"\n"+toMarkdown(tools.RedactCredentials(v.Content().String()), false, m.width)), toolMessageType)
						continue
					}
					if v.Role != message.Assistant {
						continue
					}
					if reasoning := v.ReasoningContent().Thinking; reasoning != "" {
						appendText(v.ID+":thinking", base.Foreground(t.TextMuted()).Render(toMarkdown(tools.RedactCredentials(reasoning), false, m.width)), toolMessageType)
					}
					copy := v
					copy.Parts = nil
					for _, part := range v.Parts {
						switch part.(type) {
						case message.ReasoningContent:
							continue
						case message.TextContent:
							if len(v.ToolCalls()) == 0 {
								continue
							}
						}
						copy.Parts = append(copy.Parts, part)
					}
					// Empty end-turn markers are not process messages.
					if len(copy.ToolCalls()) == 0 && copy.Content().String() == "" {
						continue
					}
					for _, item := range renderAssistantMessage(copy, i, m.messages, m.app.Messages, m.currentMsgID, false, m.width, pos) {
						appendItem(item)
					}
				}
			}
		}
		for _, final := range finals {
			v := g.messages[final]
			v.Parts = nil
			for _, part := range g.messages[final].Parts {
				if _, thinking := part.(message.ReasoningContent); !thinking {
					v.Parts = append(v.Parts, part)
				}
			}
			start := pos
			cached, ok := m.cachedContent[v.ID]
			if !ok || cached.width != m.width {
				cached = cacheItem{width: m.width, content: renderAssistantMessage(v, final, m.messages, m.app.Messages, m.currentMsgID, m.session.SummaryMessageID == v.ID, m.width, pos)}
				m.cachedContent[v.ID] = cached
			}
			for _, item := range cached.content {
				appendItem(item)
			}
			m.hits = append(m.hits, transcriptHit{start, pos - 1, "actions", v.ID, v.Content().String()})
			if gi == len(groups)-1 && final == finals[len(finals)-1] && !m.IsAgentWorking() {
				start = pos
				appendText(v.ID+":actions", base.Foreground(t.TextMuted()).Render(ansi.Truncate(config.Text("Ctrl+Y · copiar / abrir fontes", "Ctrl+Y · copy / open sources"), m.width, "…")), toolMessageType)
				m.hits = append(m.hits, transcriptHit{start, pos - 1, "actions", v.ID, v.Content().String()})
			}
		}
	}
	m.viewport.SetContent(base.Width(m.width).Render(strings.Join(rendered, "\n")))
}
func (m *messagesCmp) toggleProcess(id string) {
	if m.expanded == nil {
		m.expanded = map[string]bool{}
		m.toggled = map[string]bool{}
	}
	if !m.toggled[id] {
		m.expanded[id] = false
		if cfg := config.Get(); cfg != nil {
			m.expanded[id] = m.IsAgentWorking() && len(m.groups()) > 0 && m.groups()[len(m.groups())-1].key == id && cfg.Preferences.ProcessVisible()
		}
	}
	m.expanded[id] = !m.expanded[id]
	m.toggled[id] = true
	offset := m.viewport.YOffset
	m.renderView()
	m.viewport.SetYOffset(offset)
}
func (m *messagesCmp) transcriptMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	// The message container adds a top and left padding cell.
	row := msg.Y - 1 + m.viewport.YOffset
	for _, hit := range m.hits {
		if row >= hit.start && row < hit.end {
			if hit.kind == "process" {
				m.toggleProcess(hit.id)
				return nil
			}
			return util.CmdHandler(ResponseActionsMsg{hit.text})
		}
	}
	if m.unread && msg.Y >= m.height-2 {
		m.unread = false
		m.viewport.GotoBottom()
	}
	return nil
}
