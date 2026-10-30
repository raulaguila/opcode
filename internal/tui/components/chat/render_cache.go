package chat

import (
	"reflect"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

const messageCacheLimit = 4 << 20
const turnCacheLimit = 4 << 20

type visualIdentity struct {
	width, profile int
	theme          uintptr
	name           string
	dark           bool
	prose          int
	language       string
	process        bool
}

func currentVisualIdentity(width int) visualIdentity {
	value := reflect.ValueOf(theme.CurrentTheme())
	var identity uintptr
	if value.Kind() == reflect.Pointer {
		identity = value.Pointer()
	}
	key := visualIdentity{width: width, profile: int(lipgloss.ColorProfile()), theme: identity, name: theme.CurrentThemeName(), dark: lipgloss.HasDarkBackground()}
	if cfg := config.Get(); cfg != nil {
		key.prose = cfg.TUI.ProseWidth
		key.language = cfg.TUI.Language
		key.process = cfg.Preferences.ProcessVisible()
	}
	return key
}

type turnCacheItem struct {
	text                     string
	lines                    []string
	items                    []uiMessage
	hits                     []transcriptHit
	dependencies             []string
	version                  uint64
	active, expanded, latest bool
	summary                  string
	bytes, height            int
	used                     uint64
}
type installedTurn struct {
	key             string
	frame           turnCacheItem
	line, item, hit int
}

type transcriptCache struct {
	markdown                          styles.MarkdownCache
	markdownOwner                     string
	visual                            visualIdentity
	groups                            []turnGroup
	groupIndex                        map[string]int
	first                             *message.Message
	count                             int
	versions                          map[string]uint64
	messageTurn                       map[string]string
	toolOwners                        map[string]string
	frames                            map[string]turnCacheItem
	frameBytes, messageBytes, entries int
	clock                             uint64
	blocks                            []installedTurn
	dirty                             bool
}

func (m *messagesCmp) resetRenderCache() {
	m.cachedContent = make(map[string]cacheItem)
	m.renderCache = transcriptCache{dirty: true}
	m.uiMessages = nil
	m.hits = nil
	m.transcriptLines = nil
	m.expanded = nil
	m.toggled = nil
	m.find.normalized = nil
	m.find.normalizedBytes = 0
}
func (m *messagesCmp) ensureRenderCache() {
	visual := currentVisualIdentity(m.width)
	c := &m.renderCache
	if c.visual != visual {
		m.cachedContent = make(map[string]cacheItem)
		*c = transcriptCache{visual: visual, dirty: true}
		m.find.normalized = nil
		m.find.normalizedBytes = 0
	}
	if c.frames == nil {
		c.frames = map[string]turnCacheItem{}
		c.versions = map[string]uint64{}
		c.messageTurn = map[string]string{}
		c.toolOwners = map[string]string{}
	}
	if m.cachedContent == nil {
		m.cachedContent = map[string]cacheItem{}
	}
	if c.entries != len(m.cachedContent) {
		m.recountMessageCache()
		c.dirty = true
	}
}
func (m *messagesCmp) invalidateMessageCache(id string) {
	c := &m.renderCache
	m.deleteMessageCache(id)
	m.deleteMessageCache(processCacheKey(id))
	c.entries = len(m.cachedContent)
	c.dirty = true
	if key := c.messageTurn[id]; key != "" {
		c.versions[key]++
	}
}
func (m *messagesCmp) recountMessageCache() {
	c := &m.renderCache
	c.messageBytes = 0
	for id, item := range m.cachedContent {
		if item.bytes == 0 {
			item.bytes = len(id) + 64 + cacheContentBytes(item.content)
			m.cachedContent[id] = item
		}
		c.messageBytes += item.bytes
	}
	c.entries = len(m.cachedContent)
}
func cacheContentBytes(items []uiMessage) int {
	total := 0
	for _, item := range items {
		total += len(item.content) + len(item.ID) + 64
	}
	return total
}
func (m *messagesCmp) putMessageCache(id string, item cacheItem) {
	c := &m.renderCache
	if old, ok := m.cachedContent[id]; ok {
		c.messageBytes -= old.bytes
	}
	item.bytes = len(id) + 64 + cacheContentBytes(item.content)
	c.clock++
	item.used = c.clock
	if item.bytes > messageCacheLimit {
		delete(m.cachedContent, id)
		c.entries = len(m.cachedContent)
		return
	}
	m.cachedContent[id] = item
	c.messageBytes += item.bytes
	c.entries = len(m.cachedContent)
	for c.messageBytes > messageCacheLimit {
		m.evictMessageCache()
	}
}
func (m *messagesCmp) evictMessageCache() {
	var key string
	oldest := ^uint64(0)
	for id, item := range m.cachedContent {
		if item.used < oldest {
			oldest = item.used
			key = id
		}
	}
	item := m.cachedContent[key]
	m.renderCache.messageBytes -= item.bytes
	delete(m.cachedContent, key)
	m.renderCache.entries = len(m.cachedContent)
}
func (m *messagesCmp) touchMessageCache(id string, _ cacheItem) {
	item, ok := m.cachedContent[id]
	if !ok {
		return
	}
	m.renderCache.clock++
	item.used = m.renderCache.clock
	m.cachedContent[id] = item
}

func (m *messagesCmp) renderGroups() []turnGroup {
	c := &m.renderCache
	var first *message.Message
	if len(m.messages) > 0 {
		first = &m.messages[0]
	}
	if first == c.first && len(m.messages) == c.count {
		return c.groups
	}
	c.first, c.count = first, len(m.messages)
	c.groups = nil
	c.groupIndex = map[string]int{}
	c.messageTurn = map[string]string{}
	c.toolOwners = map[string]string{}
	start := 0
	for index, item := range m.messages {
		if item.Role == message.User && !item.IsApplication() {
			if index > start {
				m.appendRenderGroup(start, index)
			}
			start = index
		}
	}
	if len(m.messages) > start {
		m.appendRenderGroup(start, len(m.messages))
	}
	c.dirty = true
	return c.groups
}
func (m *messagesCmp) appendRenderGroup(start, end int) {
	c := &m.renderCache
	items := m.messages[start:end]
	g := turnGroup{key: items[0].ID, messages: items}
	if items[0].Role == message.User && !items[0].IsApplication() {
		g.user = &items[0]
		g.messages = items[1:]
	}
	c.groupIndex[g.key] = len(c.groups)
	c.groups = append(c.groups, g)
	for _, item := range items {
		c.messageTurn[item.ID] = g.key
		for _, call := range item.ToolCalls() {
			c.toolOwners[call.ID] = item.ID
		}
	}
}
func (m *messagesCmp) frameValid(frame turnCacheItem, g turnGroup, active, expanded, latest bool) bool {
	if frame.latest != latest || frame.version != m.renderCache.versions[g.key] || frame.active != active || frame.expanded != expanded || frame.summary != m.session.SummaryMessageID {
		return false
	}
	return true
}
func (m *messagesCmp) storeTurnFrame(key string, frame turnCacheItem) {
	c := &m.renderCache
	if old, ok := c.frames[key]; ok {
		c.frameBytes -= old.bytes
		delete(c.frames, key)
	}
	frame.bytes = len(frame.text) + len(frame.lines)*16 + cacheContentBytes(frame.items) + len(frame.hits)*64 + len(key) + 128
	for _, id := range frame.dependencies {
		frame.bytes += len(id) + 16
	}
	if frame.bytes > turnCacheLimit {
		return
	}
	c.clock++
	frame.used = c.clock
	c.frames[key] = frame
	c.frameBytes += frame.bytes
	for c.frameBytes > turnCacheLimit {
		m.evictTurnFrame()
	}
}
func (m *messagesCmp) evictTurnFrame() {
	c := &m.renderCache
	var key string
	oldest := ^uint64(0)
	for id, item := range c.frames {
		if item.used < oldest {
			key = id
			oldest = item.used
		}
	}
	c.frameBytes -= c.frames[key].bytes
	delete(c.frames, key)
}
func (m *messagesCmp) appendTurnFrame(frame turnCacheItem, offset int) {
	for _, item := range frame.items {
		item.position += offset
		m.uiMessages = append(m.uiMessages, item)
	}
	for _, hit := range frame.hits {
		hit.start += offset
		hit.end += offset
		m.hits = append(m.hits, hit)
	}
}

// The flattened slice contains line references, never a second transcript string.
func (m *messagesCmp) truncateTranscript(blocks []installedTurn, index int) {
	line, item, hit := len(m.transcriptLines), len(m.uiMessages), len(m.hits)
	if index == 0 {
		line, item, hit = 0, 0, 0
	} else if index < len(blocks) {
		line, item, hit = blocks[index].line, blocks[index].item, blocks[index].hit
	}
	clear(m.transcriptLines[line:])
	m.transcriptLines = m.transcriptLines[:line]
	clear(m.uiMessages[item:])
	m.uiMessages = m.uiMessages[:item]
	clear(m.hits[hit:])
	m.hits = m.hits[:hit]
	m.renderCache.blocks = m.renderCache.blocks[:min(index, len(m.renderCache.blocks))]
}

func (m *messagesCmp) installTurnFrame(key string, frame turnCacheItem) {
	block := installedTurn{key, frame, len(m.transcriptLines), len(m.uiMessages), len(m.hits)}
	m.transcriptLines = append(m.transcriptLines, frame.lines...)
	m.appendTurnFrame(frame, block.line)
	m.renderCache.blocks = append(m.renderCache.blocks, block)
}

func (m *messagesCmp) installTranscriptLines() {
	if m.find.open {
		m.updateSearch()
	}
	m.viewport.SetLines(m.transcriptLines)
}

func processCacheKey(id string) string { return id + "\x00process" }
func (m *messagesCmp) deleteMessageCache(id string) {
	if old, ok := m.cachedContent[id]; ok {
		m.renderCache.messageBytes -= old.bytes
		delete(m.cachedContent, id)
	}
	m.renderCache.entries = len(m.cachedContent)
}
func (m *messagesCmp) invalidateToolResponses(item message.Message) {
	for _, result := range item.ToolResults() {
		if owner := m.renderCache.toolOwners[result.ToolCallID]; owner != "" {
			m.invalidateMessageCache(owner)
		}
	}
}
func (m *messagesCmp) invalidateProcessCaches() {
	for id := range m.cachedContent {
		if strings.HasSuffix(id, "\x00process") {
			m.deleteMessageCache(id)
		}
	}
}
