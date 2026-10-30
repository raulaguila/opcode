package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

type renderHistory struct {
	message.Service
	histories map[string][]message.Message
	calls     int
}

func (s *renderHistory) List(_ context.Context, id string) ([]message.Message, error) {
	s.calls++
	return s.histories[id], nil
}
func renderFixture(count int) *messagesCmp {
	m := NewMessagesCmp(&app.App{CoderAgent: benchmarkAgent{}}).(*messagesCmp)
	m.session = session.Session{ID: "A"}
	m.SetSize(100, 30)
	for i := range count {
		role := message.Assistant
		if i%2 == 0 {
			role = message.User
		}
		m.messages = append(m.messages, message.Message{ID: fmt.Sprint(i), SessionID: "A", Role: role, Parts: []message.ContentPart{message.TextContent{Text: "A bounded example message with source references and ordinary project information. ação 漢字"}}})
	}
	m.renderView()
	return m
}
func TestIncrementalTranscriptMatchesFullRenderingAndHitPositions(t *testing.T) {
	m := renderFixture(12)
	// The reference renders all turns and applies the full-width base once, as the previous renderer did.
	reference := turnRenderer{messages: m, base: styles.BaseStyle(), theme: theme.CurrentTheme()}
	groups := m.groups()
	for i, g := range groups {
		reference.renderGroup(g, i == len(groups)-1)
	}
	expected := reference.base.Width(m.width).Render(strings.Join(reference.rendered, "\n"))
	require.Equal(t, expected, strings.Join(m.transcriptLines, "\n"))
	require.Equal(t, reference.hits, m.hits)
	require.Equal(t, reference.items, m.uiMessages)
	content := strings.Join(m.transcriptLines, "\n")
	first := m.renderCache.frames["0"]
	last := m.messages[len(m.messages)-1]
	last.AppendContent(" updated")
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	require.NotEqual(t, content, strings.Join(m.transcriptLines, "\n"))
	cached := m.renderCache.frames["0"]
	first.used = cached.used
	require.Equal(t, first, cached, "the immutable prefix should be reused")
	require.Same(t, &first.items[0], &cached.items[0])
	require.Contains(t, ansi.Strip(strings.Join(m.transcriptLines, "\n")), "updated")
}
func TestRenderCacheSessionThemeAndResizeIdentity(t *testing.T) {
	old := theme.CurrentThemeName()
	t.Cleanup(func() { _ = theme.PreviewTheme(old) })
	history := &renderHistory{histories: map[string][]message.Message{
		"A": {{ID: "same", SessionID: "A", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "session A"}}}},
		"B": {{ID: "same", SessionID: "B", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "session B"}}}}}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(40, 20)
	m.Update(m.SetSession(session.Session{ID: "A"})())
	require.Contains(t, ansi.Strip(strings.Join(m.transcriptLines, "\n")), "session A")
	m.Update(m.SetSession(session.Session{ID: "B"})())
	require.NotContains(t, ansi.Strip(strings.Join(m.transcriptLines, "\n")), "session A")
	require.NoError(t, theme.PreviewTheme("gruvbox"))
	m.rerender()
	m.Update(m.SetSession(session.Session{ID: "A"})())
	require.Equal(t, theme.CurrentThemeName(), m.renderCache.visual.name)
	require.NotContains(t, ansi.Strip(strings.Join(m.transcriptLines, "\n")), "session B")
	m.SetSize(20, 20)
	require.Equal(t, 20, m.renderCache.visual.width)
	for _, line := range m.transcriptLines {
		require.LessOrEqual(t, lipgloss.Width(line), 20)
	}
	require.Len(t, m.cachedContent, 1)
}
func TestRenderCacheBudgetsAndSearchUnicode(t *testing.T) {
	m := renderFixture(2)
	for i := range 10 {
		m.putMessageCache(fmt.Sprint(i), cacheItem{content: []uiMessage{{content: strings.Repeat("x", messageCacheLimit/3)}}})
	}
	require.LessOrEqual(t, m.renderCache.messageBytes, messageCacheLimit)
	for i := range 10 {
		m.storeTurnFrame(fmt.Sprint(i), turnCacheItem{text: strings.Repeat("x", turnCacheLimit/3)})
	}
	require.LessOrEqual(t, m.renderCache.frameBytes, turnCacheLimit)
	m.rerender()
	m.StartSearch("AÇÃO")
	require.NotEmpty(t, m.find.matches)
	normalized := m.find.normalizedBytes
	m.updateSearch()
	require.Equal(t, normalized, m.find.normalizedBytes)
	require.LessOrEqual(t, m.find.normalizedBytes, normalizedSearchLimit)
	m.SetSize(60, 20)
	require.GreaterOrEqual(t, m.find.normalizedBytes, 0)
	m.resetRenderCache()
	require.Zero(t, m.find.normalizedBytes)
}
func TestOverflowReloadRejectsStaleSessionAndEventGeneration(t *testing.T) {
	m := renderFixture(2)
	history := &renderHistory{histories: map[string][]message.Message{"A": m.messages}}
	m.app.Messages = history
	cmd := m.reconcileMessageEvent(pubsub.Event[message.Message]{ReconcileRequired: true})
	require.NotNil(t, cmd)
	snapshot := cmd().(messageReloadMsg)
	m.reconcile.version++
	require.NotNil(t, m.acceptMessageReload(snapshot), "an event received during the query requires a new authoritative read")
	require.True(t, m.reconcile.pending)
	m.resetMessageReconciliation()
	require.Nil(t, m.acceptMessageReload(snapshot))
	require.False(t, m.reconcile.pending)
}
func TestRendererNeverReadsPersistenceAndProjectionTerminatesCycles(t *testing.T) {
	m := renderFixture(2)
	history := &renderHistory{}
	m.app.Messages = history
	m.messages = append(m.messages, message.Message{ID: "parent", SessionID: "A", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "child", Name: agent.AgentToolName, Finished: true, Input: `{"prompt":"Review safely"}`}}})
	m.nested.snapshots = map[string][]message.Message{"child": {{ID: "nested", SessionID: "child", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "nested-tool", Name: "bash", Input: `{"command":"echo projection"}`, Finished: true}}}}}
	m.rerender()
	m.toggleProcess("0")
	require.Contains(t, ansi.Strip(strings.Join(m.transcriptLines, "\n")), "echo projection")
	require.Zero(t, history.calls)
	projection := &nestedMessageProjection{snapshots: map[string][]message.Message{"a": m.messages}, visited: map[string]bool{}, remaining: 2}
	messages, err := projection.List(context.Background(), "a")
	require.NoError(t, err)
	require.NotEmpty(t, messages)
	messages, err = projection.List(context.Background(), "a")
	require.NoError(t, err)
	require.Empty(t, messages)
}
func BenchmarkTranscriptRenderWarm(b *testing.B) {
	m := renderFixture(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.renderView()
	}
}
func BenchmarkTranscriptRenderTailInvalidated(b *testing.B) {
	m := renderFixture(1000)
	id := m.messages[len(m.messages)-1].ID
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.invalidateMessageCache(id)
		m.renderView()
	}
}
func BenchmarkTranscriptRenderStreaming(b *testing.B) {
	m := renderFixture(1000)
	last := m.messages[len(m.messages)-1]
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		update := last
		update.Parts = []message.ContentPart{message.TextContent{Text: fmt.Sprintf("A live answer with source references and ordinary project information. chunk %d", i)}}
		m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: update})
	}
}

func TestExpandedProcessCacheInvalidatesOnToolResponse(t *testing.T) {
	m := renderFixture(0)
	m.messages = []message.Message{
		{ID: "u", SessionID: "A", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "read file"}}},
		{ID: "call", SessionID: "A", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "tool", Name: "bash", Input: `{"command":"printf evidence"}`, Finished: true}}},
	}
	m.renderView()
	m.toggleProcess("u")
	cached, ok := m.cachedContent[processCacheKey("call")]
	require.True(t, ok)
	require.NotEmpty(t, cached.content)
	result := message.Message{ID: "tool-result", SessionID: "A", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "tool", Content: "new evidence from repository", Metadata: `{"content":"new evidence from repository","file_path":"README.md"}`}}}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: result})
	require.Contains(t, ansi.Strip(strings.Join(m.transcriptLines, "\n")), "new evidence from repository")
	require.NotEqual(t, cached.content, m.cachedContent[processCacheKey("call")].content)
}
func TestFrozenSelectionReceivesLatestRenderAfterClearing(t *testing.T) {
	m := renderFixture(2)
	old := strings.Join(m.transcriptLines, "\n")
	m.selection = textSelection{anchor: textPoint{0, 0}, focus: textPoint{3, 0}, moved: true}
	last := m.messages[1]
	last.AppendContent(" UPDATED_TAIL")
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	require.Equal(t, old, strings.Join(m.transcriptLines, "\n"))
	m.ClearSelection()
	require.Contains(t, ansi.Strip(strings.Join(m.transcriptLines, "\n")), "UPDATED_TAIL")
}

func TestTaskHistoryDiscoveryOnUpdatedMessageAndStaleReload(t *testing.T) {
	m := renderFixture(2)
	history := &renderHistory{histories: map[string][]message.Message{}}
	m.app.Messages = history
	event := pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: message.Message{ID: "1", SessionID: "A", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "child", Name: agent.AgentToolName}}}}
	m.messages[1] = event.Payload
	command := m.nestedMessageEvent(event)
	require.NotNil(t, command)
	result := command().(nestedHistoryMsg)
	require.Nil(t, m.nestedMessageEvent(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: message.Message{SessionID: "child"}}))
	require.True(t, m.nested.again)
	require.NotNil(t, m.acceptNestedHistory(result), "a task event arriving during the read must trigger a fresh snapshot")
	require.Nil(t, m.nested.snapshots, "the older query must not overwrite a newer task event")
}
