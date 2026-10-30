package chat

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
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
	for _, v := range slices.Backward(m.messages) {

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

type turnRenderer struct {
	messages     *messagesCmp
	base         lipgloss.Style
	theme        theme.Theme
	rendered     []string
	position     int
	items        []uiMessage
	hits         []transcriptHit
	dependencies []string
}

func (m *messagesCmp) renderTurns() {
	if m.rendering || m.selection.engaged() || m.width < 1 {
		return
	}
	m.ensureRenderCache()
	groups := m.visibleRenderGroups()
	working := len(m.messages) > 0 && m.IsAgentWorking()
	if !m.renderCache.dirty && m.lastWorking == working {
		return
	}
	m.lastWorking = working
	blocks := m.renderCache.blocks
	changed := false
	for index, group := range groups {
		frame, reused := m.renderTurnFrame(group, blocks, index, index == len(groups)-1, working)
		if !reused && !changed {
			m.truncateTranscript(blocks, index)
			changed = true
		}
		if changed {
			m.installTurnFrame(group.key, frame)
		}
	}
	if len(groups) < len(blocks) {
		if !changed {
			m.truncateTranscript(blocks, len(groups))
		}
		clear(blocks[len(groups):])
		changed = true
	}
	m.renderCache.dirty = false
	if changed {
		m.installTranscriptLines()
	}
}
func (m *messagesCmp) renderTurnFrame(g turnGroup, blocks []installedTurn, index int, latest, working bool) (turnCacheItem, bool) {
	active := latest && working
	renderer := turnRenderer{messages: m}
	expanded := renderer.processExpanded(g, active)
	if index < len(blocks) && blocks[index].key == g.key && m.frameValid(blocks[index].frame, g, active, expanded, latest) {
		return blocks[index].frame, true
	}
	if frame, ok := m.renderCache.frames[g.key]; ok && m.frameValid(frame, g, active, expanded, latest) {
		m.renderCache.clock++
		frame.used = m.renderCache.clock
		m.renderCache.frames[g.key] = frame
		return frame, false
	}
	renderer.base, renderer.theme = styles.BaseStyle(), theme.CurrentTheme()
	renderer.renderGroup(g, latest)
	frame := turnCacheItem{text: renderer.base.Width(m.width).Render(strings.Join(renderer.rendered, "\n")), version: m.renderCache.versions[g.key], active: active, expanded: expanded, latest: latest, summary: m.session.SummaryMessageID}
	// A frame owns relative positions independently of the assembled transcript.
	frame.items = renderer.items
	frame.hits = renderer.hits
	frame.dependencies = renderer.dependencies
	frame.lines = strings.Split(frame.text, "\n")
	frame.height = len(frame.lines)
	m.storeTurnFrame(g.key, frame)
	return frame, false
}

func (r *turnRenderer) appendItem(item uiMessage) {
	item.position = r.position
	item.height = lipgloss.Height(item.content)
	r.items = append(r.items, item)
	r.rendered = append(r.rendered, item.content, "")
	r.position += item.height + 1
}

func (r *turnRenderer) appendText(id, text string) {
	r.appendItem(uiMessage{ID: id, messageType: toolMessageType, content: r.base.Width(r.messages.width).Render(text)})
}

func (r *turnRenderer) renderGroup(group turnGroup, latest bool) {
	r.renderUser(group)
	finals, hasProcess := turnFinals(group.messages)
	if hasProcess {
		r.renderProcess(group, latest && r.messages.IsAgentWorking())
	}
	for index, final := range finals {
		r.renderFinal(group.messages[final], final, latest && index == len(finals)-1)
	}
}

func (r *turnRenderer) renderUser(group turnGroup) {
	if group.user == nil {
		return
	}
	m := r.messages
	cached, ok := m.cachedContent[group.user.ID]
	if !ok || cached.width != m.width {
		cached = cacheItem{width: m.width, content: []uiMessage{renderUserMessage(*group.user, group.key == m.currentMsgID, m.width, r.position)}}
		m.putMessageCache(group.user.ID, cached)
	}
	r.dependencies = append(r.dependencies, group.user.ID)
	m.touchMessageCache(group.user.ID, cached)
	for _, item := range cached.content {
		r.appendItem(item)
	}
}

func turnFinals(messages []message.Message) (finals []int, process bool) {
	for index, item := range messages {
		process = process || len(item.ToolCalls()) > 0 || item.IsApplication() || (item.Role == message.Assistant && item.ReasoningContent().Thinking != "")
		if item.IsApplication() {
			finals = nil
		}
		if item.Role == message.Assistant && len(item.ToolCalls()) == 0 && item.Content().String() != "" {
			finals = append(finals, index)
		}
	}
	return finals, process
}

func (r *turnRenderer) processExpanded(group turnGroup, active bool) bool {
	m := r.messages
	if m.toggled[group.key] {
		return m.expanded[group.key]
	}
	cfg := config.Get()
	return cfg != nil && active && cfg.Preferences.ProcessVisible()
}

func (r *turnRenderer) renderProcess(group turnGroup, active bool) {
	m := r.messages
	expanded := r.processExpanded(group, active)
	mark := "›"
	if expanded {
		mark = "⌄"
	}
	label := fmt.Sprintf(config.Text("%s Processo · %s", "%s Process · %s"), mark, processSummary(group.messages))
	if active {
		label += " · " + config.Text("em andamento", "working")
	}
	start := r.position
	r.appendText(group.key+":process", r.base.Foreground(r.theme.TextMuted()).Render(ansi.Truncate(label, m.width, "…")))
	r.hits = append(r.hits, transcriptHit{start, start + 1, "process", group.key, ""})
	if expanded {
		for index, item := range group.messages {
			r.renderProcessMessage(item, index)
		}
	}
}

func processMessage(item message.Message) message.Message {
	copy := item
	copy.Parts = nil
	for _, part := range item.Parts {
		switch part.(type) {
		case message.ReasoningContent:
			continue
		case message.TextContent:
			if len(item.ToolCalls()) == 0 {
				continue
			}
		}
		copy.Parts = append(copy.Parts, part)
	}
	return copy
}

func (r *turnRenderer) renderProcessMessage(item message.Message, index int) {
	if item.Role != message.Assistant && !item.IsApplication() {
		return
	}
	m := r.messages
	key := processCacheKey(item.ID)
	cached, ok := m.cachedContent[key]
	if !ok || cached.width != m.width {
		child := turnRenderer{messages: m, base: r.base, theme: r.theme}
		child.renderProcessMessageContent(item, index)
		cached = cacheItem{width: m.width, content: child.items}
		m.putMessageCache(key, cached)
	}
	r.dependencies = append(r.dependencies, key)
	m.touchMessageCache(key, cached)
	for _, rendered := range cached.content {
		r.appendItem(rendered)
	}
}
func (r *turnRenderer) renderProcessMessageContent(item message.Message, index int) {
	m := r.messages
	if item.IsApplication() {
		r.appendText(item.ID, r.base.Foreground(r.theme.TextMuted()).Render(config.Text("Validação da aplicação", "Application validation")+"\n"+toMarkdown(tools.RedactCredentials(item.Content().String()), false, m.width)))
		return
	}
	if item.Role != message.Assistant {
		return
	}
	if reasoning := item.ReasoningContent().Thinking; reasoning != "" {
		r.appendText(item.ID+":thinking", r.base.Foreground(r.theme.TextMuted()).Render(toMarkdown(tools.RedactCredentials(reasoning), false, m.width)))
	}
	copy := processMessage(item)
	if len(copy.ToolCalls()) == 0 && copy.Content().String() == "" {
		return
	}
	for _, rendered := range renderAssistantMessage(copy, index, m.messages, m.nestedProjection(), m.currentMsgID, false, m.width, r.position) {
		r.appendItem(rendered)
	}
}

func withoutReasoning(item message.Message) message.Message {
	parts := item.Parts
	item.Parts = nil
	for _, part := range parts {
		if _, thinking := part.(message.ReasoningContent); !thinking {
			item.Parts = append(item.Parts, part)
		}
	}
	return item
}

func (r *turnRenderer) renderFinal(item message.Message, index int, latest bool) {
	m := r.messages
	start := r.position
	cached, ok := m.cachedContent[item.ID]
	if !ok || cached.width != m.width {
		cached = cacheItem{width: m.width, content: renderAssistantMessage(withoutReasoning(item), index, m.messages, m.nestedProjection(), m.currentMsgID, m.session.SummaryMessageID == item.ID, m.width, r.position)}
		m.putMessageCache(item.ID, cached)
	}
	r.dependencies = append(r.dependencies, item.ID)
	m.touchMessageCache(item.ID, cached)
	for _, rendered := range cached.content {
		r.appendItem(rendered)
	}
	r.hits = append(r.hits, transcriptHit{start, r.position - 1, "actions", item.ID, item.Content().String()})
	if latest && !m.IsAgentWorking() {
		r.renderActions(item)
	}
}

func (r *turnRenderer) renderActions(item message.Message) {
	start := r.position
	text := config.Text("Arraste para selecionar · Ctrl+C copia · Ctrl+Y ações", "Drag to select · Ctrl+C copies · Ctrl+Y actions")
	r.appendText(item.ID+":actions", r.base.Foreground(r.theme.TextMuted()).Render(ansi.Truncate(text, r.messages.width, "…")))
	r.hits = append(r.hits, transcriptHit{start, r.position - 1, "actions", item.ID, item.Content().String()})
}

func (m *messagesCmp) toggleProcess(id string) {
	m.ClearSelection()
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
	m.renderCache.dirty = true
	offset := m.viewport.YOffset
	m.renderView()
	m.viewport.SetYOffset(offset)
}
func (m *messagesCmp) transcriptMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		m.selectionMouse(msg)
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
		}
	}
	if m.unread && msg.Y >= m.height-2 {
		m.ClearSelection()
		m.unread = false
		m.viewport.GotoBottom()
		return nil
	}
	m.selectionMouse(msg)
	return nil
}
