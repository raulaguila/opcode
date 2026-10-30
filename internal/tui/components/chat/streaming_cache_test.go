package chat

import (
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"testing"
)

func TestCompletedReplyDropsLiveMarkdownCache(t *testing.T) {
	m := renderFixture(2)
	last := m.messages[len(m.messages)-1]
	last.Parts = []message.ContentPart{message.TextContent{Text: "Stable prose.\n\nCurrent paragraph."}}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	if m.renderCache.markdownOwner != last.ID {
		t.Fatal("live response cache not owned by current reply")
	}
	last.Parts = append(last.Parts, message.Finish{Reason: message.FinishReasonEndTurn, Time: 1})
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	if m.renderCache.markdownOwner != "" {
		t.Fatal("completed response retained live cache")
	}
	assertTranscriptReference(t, m)
}
