package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func assertTranscriptReference(t *testing.T, m *messagesCmp) {
	t.Helper()
	r := turnRenderer{messages: m, base: styles.BaseStyle(), theme: theme.CurrentTheme()}
	groups := m.groups()
	for index, group := range groups {
		r.renderGroup(group, index == len(groups)-1)
	}
	require.Equal(t, r.base.Width(m.width).Render(strings.Join(r.rendered, "\n")), strings.Join(m.transcriptLines, "\n"))
	require.Equal(t, r.items, m.uiMessages)
	require.Equal(t, r.hits, m.hits)
}

func TestGrowingAndShrinkingReplyKeepsPrefixLinesAndExactCoordinates(t *testing.T) {
	m := renderFixture(20)
	prefix := m.renderCache.blocks[0].frame.lines
	last := m.messages[len(m.messages)-1]
	for index := range 12 {
		last.AppendContent("\n\n" + strings.Repeat("Evidence with wide 漢字 and [source](https://example.test). ", index+1))
		m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
		require.Same(t, &prefix[0], &m.renderCache.blocks[0].frame.lines[0])
		assertTranscriptReference(t, m)
	}
	last.Parts = []message.ContentPart{message.TextContent{Text: "short reply"}}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	assertTranscriptReference(t, m)
}

func TestInstalledTranscriptSurvivesBothCacheBudgets(t *testing.T) {
	m := renderFixture(20)
	prefix := m.renderCache.blocks[0].frame.lines
	for index := range 4 {
		key := fmt.Sprintf("eviction-%d", index)
		text := strings.Repeat("x", messageCacheLimit/2)
		m.putMessageCache(key, cacheItem{content: []uiMessage{{content: text}}})
		m.storeTurnFrame(key, turnCacheItem{text: text})
	}
	require.NotContains(t, m.cachedContent, "0")
	require.NotContains(t, m.renderCache.frames, "0")
	last := m.messages[len(m.messages)-1]
	last.AppendContent(" new streamed evidence")
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	require.Same(t, &prefix[0], &m.renderCache.blocks[0].frame.lines[0])
	require.LessOrEqual(t, m.renderCache.messageBytes, messageCacheLimit)
	require.LessOrEqual(t, m.renderCache.frameBytes, turnCacheLimit)
	assertTranscriptReference(t, m)
	m.StartSearch("AÇÃO")
	require.NotEmpty(t, m.find.matches)
}

func TestEarlierTurnChangeMovesFollowingHitsAndLineReferences(t *testing.T) {
	m := renderFixture(20)
	tail := m.renderCache.blocks[len(m.renderCache.blocks)-1].frame.lines
	first := m.messages[1]
	first.AppendContent(strings.Repeat("\n\nEarlier evidence changes the turn height.", 8))
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: first})
	require.Same(t, &tail[0], &m.renderCache.blocks[len(m.renderCache.blocks)-1].frame.lines[0])
	assertTranscriptReference(t, m)
}

func TestRemovingTurnsKeepsNewLatestActionsAndExactCoordinates(t *testing.T) {
	m := renderFixture(20)
	m.messages = m.messages[:len(m.messages)-2]
	m.renderView()
	assertTranscriptReference(t, m)
	require.Len(t, m.renderCache.blocks, 9)
	require.NotEmpty(t, m.hits)
	require.Equal(t, "17", m.hits[len(m.hits)-1].id)
}
