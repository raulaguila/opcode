package chat

import (
	"fmt"
	"github.com/raulaguila/opcode/internal/tui/util"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	start, end int
	kind, id   string
	expanded   bool
}
type TranscriptControl interface{ LatestResponse() string }
type turnGroup struct {
	user     *message.Message
	messages []message.Message
	key      string
}

func (m *messagesCmp) LatestResponse() string {
	return latestRevisionResponse(m.groups())
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
	active       bool
	groupKey     string
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
	r.groupKey = group.key
	r.renderUser(group)
	r.active = latest && r.messages.IsAgentWorking()
	revisions := planResponseRevisions(group.messages)
	start, first := -1, true
	lastPublic := revisions.latest(group.messages)
	for index, item := range group.messages {
		answer := answeredQuestions(item, r.messages.renderCache.toolOwners, r.messages.messages)
		_, hidden := revisions.hidden[index]
		if !hidden && (publicResponse(item) || len(answer) > 0) {
			if start >= 0 {
				r.renderProcessRange(group, start, index, first, false)
				first, start = false, -1
			}
			r.renderTimelineItem(group.messages, index, latest && index == lastPublic, answer, revisions)
		}
		if !hidden && len(answer) == 0 && processEvent(item) && start < 0 {
			start = index
		}
	}
	if start >= 0 {
		r.renderProcessRange(group, start, len(group.messages), first, latest && r.messages.IsAgentWorking())
	}
}

func (r *turnRenderer) renderTimelineItem(items []message.Message, index int, latest bool, answer []answeredQuestion, revisions responseRevisionPlan) {
	if revision := revisions.roots[index]; revision != nil {
		r.renderRevision(items, revision, latest)
		return
	}
	if legacyPreviousVersion(items, index) {
		if r.renderRevisionToggle(items[index].ID, config.Text("Ver versão anterior", "View previous version")) {
			r.renderFinal(items[index], index, false)
		}
		return
	}
	r.renderPublicItem(items[index], index, latest, answer)
}

func lastPublicIndex(items []message.Message) int {
	last := -1
	for i, item := range items {
		if publicResponse(item) {
			last = i
		}
	}
	return last
}

func (r *turnRenderer) renderPublicItem(item message.Message, index int, latest bool, answer []answeredQuestion) {
	if len(answer) == 0 {
		r.renderFinal(item, index, latest)
		return
	}
	r.appendItem(uiMessage{ID: item.ID + ":answer", messageType: userMessageType, content: renderAnsweredQuestions(answer, r.messages.width)})
}

func publicResponse(item message.Message) bool {
	return item.Role == message.Assistant && item.Content().String() != ""
}

func processEvent(item message.Message) bool {
	return item.IsApplication() || item.Role == message.Tool || len(item.ToolCalls()) > 0 || (item.ReasoningContent().Thinking != "" && !publicResponse(item))
}

func (r *turnRenderer) renderProcessRange(group turnGroup, start, end int, first, active bool) {
	key := group.key
	if !first {
		key += "\x00process:" + group.messages[start].ID
	}
	r.renderProcess(turnGroup{key: key, messages: group.messages[start:end]}, active)
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
	status := ""
	if reasoningOnly(group.messages) && (!active || completedReasoning(group.messages)) {
		label = mark + " " + config.Text("Detalhes", "Details")
	} else if active && !completedReasoning(group.messages) {
		status = " · " + config.Text("em andamento", "working")
	} else {
		status = " · " + config.Text("finalizado", "finished")
	}
	label = styles.HintOn(r.theme.Background()).Render(label) + r.base.Render(status)
	start := r.position
	r.appendText(group.key+":process", ansi.Truncate(label, m.width, "…"))
	r.hits = append(r.hits, transcriptHit{start: start, end: start + 1, kind: "process", id: group.key, expanded: expanded})
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
			continue
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
	if reasoning := item.ReasoningContent().Thinking; reasoning != "" && !publicResponse(item) {
		label := config.Text("Raciocínio interno", "Internal reasoning")
		r.appendText(item.ID+":thinking", r.base.Foreground(r.theme.TextMuted()).Render(label+"\n"+toMarkdown(tools.RedactCredentials(reasoning), false, m.width)))
	}
	copy := processMessage(item)
	if len(copy.ToolCalls()) == 0 && copy.Content().String() == "" {
		return
	}
	for _, rendered := range renderAssistantMessage(copy, index, m.messages, m.nestedProjection(), m.currentMsgID, false, m.width, r.position) {
		r.appendItem(rendered)
	}
}

func publicMessage(item message.Message) message.Message {
	parts := item.Parts
	item.Parts = nil
	for _, part := range parts {
		switch part.(type) {
		case message.ReasoningContent, message.ToolCall:
			continue
		default:
			item.Parts = append(item.Parts, part)
		}
	}
	return item
}

func (r *turnRenderer) renderFinal(item message.Message, index int, latest bool) {
	m := r.messages
	r.renderReasoningDetails(item)
	start := r.position
	var markdown *styles.MarkdownCache
	if latest && !item.IsFinished() {
		if m.renderCache.markdownOwner != item.ID {
			m.renderCache.markdown.Reset()
			m.renderCache.markdownOwner = item.ID
		}
		markdown = &m.renderCache.markdown
	} else if m.renderCache.markdownOwner == item.ID {
		m.renderCache.markdown.Reset()
		m.renderCache.markdownOwner = ""
	}
	cached, ok := m.cachedContent[item.ID]
	if !ok || cached.width != m.width {
		display := publicMessage(item)
		if item.CitationValidation() == message.CitationRevising {
			display.SetCitationValidation("")
		}
		cached = cacheItem{width: m.width, content: renderAssistantMessage(display, index, m.messages, m.nestedProjection(), m.currentMsgID, m.session.SummaryMessageID == item.ID, m.width, r.position, markdown)}
		m.putMessageCache(item.ID, cached)
	}
	r.dependencies = append(r.dependencies, item.ID)
	m.touchMessageCache(item.ID, cached)
	for _, rendered := range cached.content {
		r.appendItem(rendered)
	}
	r.renderRevisingState(item)
	r.hits = append(r.hits, transcriptHit{start: start, end: r.position - 1, kind: "actions", id: item.ID})
	if latest && !m.IsAgentWorking() {
		r.renderActions(item)
	}
}

func (r *turnRenderer) renderActions(item message.Message) {
	start := r.position
	text := config.Text("Arraste para selecionar · Ctrl+C copia · Ctrl+Y ações", "Drag to select · Ctrl+C copies · Ctrl+Y actions")
	r.appendText(item.ID+":actions", styles.HintOn(r.theme.Background()).Render(ansi.Truncate(text, r.messages.width, "…")))
	r.hits = append(r.hits, transcriptHit{start: start, end: r.position - 1, kind: "actions", id: item.ID})
}

func (m *messagesCmp) toggleProcess(id string) {
	expanded := m.expanded[id]
	if !m.toggled[id] {
		expanded = m.renderedProcessExpanded(id)
	}
	m.ClearSelection()
	if m.expanded == nil {
		m.expanded = map[string]bool{}
		m.toggled = map[string]bool{}
	}
	m.expanded[id] = !expanded
	m.toggled[id] = true
	parent, _, _ := strings.Cut(id, "\x00process:")
	m.renderCache.versions[parent]++
	m.renderCache.dirty = true
	offset := m.viewport.YOffset
	m.renderView()
	m.viewport.SetYOffset(offset)
}

func (m *messagesCmp) renderedProcessExpanded(id string) bool {
	for _, hit := range m.hits {
		if hit.kind == "process" && hit.id == id {
			return hit.expanded
		}
	}
	return false
}
func (m *messagesCmp) transcriptMouse(msg tea.MouseMsg) tea.Cmd {
	if !util.MousePressed(msg) || msg.Mouse().Button != tea.MouseLeft {
		m.selectionMouse(msg)
		return nil
	}
	// The message container adds a top and left padding cell.
	row := msg.Mouse().Y - 1 + m.viewport.YOffset
	for _, hit := range m.hits {
		if row >= hit.start && row < hit.end {
			if hit.kind == "process" {
				m.toggleProcess(hit.id)
				return nil
			}
			if hit.kind == "revision" {
				m.toggleRevision(hit.id)
				return nil
			}
		}
	}
	if m.unread && msg.Mouse().Y >= m.height-2 {
		m.ClearSelection()
		m.unread = false
		m.viewport.GotoBottom()
		return nil
	}
	m.selectionMouse(msg)
	return nil
}
