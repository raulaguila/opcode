package message

import (
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/pubsub"
)

// StreamPublisher publishes an immutable live snapshot without a database write.
// The owner must still call Service.Update at checkpoints and stream boundaries.
// Service remains compatible with stores that only implement durable updates.
type StreamPublisher interface {
	PublishUpdate(Message)
}

func (s *service) PublishUpdate(msg Message) {
	msg.UpdatedAt = time.Now().Unix()
	snapshot := snapshotMessage(msg)
	s.streamMu.Lock()
	if s.streams == nil {
		s.streams = make(map[string]Message)
	}
	s.streams[msg.ID] = snapshot
	s.streamMu.Unlock()
	s.Publish(pubsub.UpdatedEvent, snapshot)
}

// Get/List also serve live snapshots so a UI recovering dropped notifications
// does not regress to the previous SQL checkpoint. The overlay is transient;
// every successful durable update removes its already-persisted entry.
func (s *service) liveMessage(persisted Message) Message {
	s.streamMu.RLock()
	defer s.streamMu.RUnlock()
	if live, ok := s.streams[persisted.ID]; ok {
		return snapshotMessage(live)
	}
	return persisted
}

func (s *service) forgetStream(id string) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	delete(s.streams, id)
}

// StreamBuffer belongs to a single streaming worker. It accumulates fragments
// without copying the response prefix on every token. Materialize exposes only
// immutable strings; further builder writes cannot change earlier snapshots.
// Callers must not replace/reorder text parts while this buffer is in use.
type StreamBuffer struct {
	message         *Message
	text, reasoning []*streamPart
}

type streamPart struct {
	index  int
	origin string
	value  strings.Builder
}

func NewStreamBuffer(msg *Message) *StreamBuffer { return &StreamBuffer{message: msg} }

func (b *StreamBuffer) AppendText(delta string) {
	if b.text == nil {
		for i, part := range b.message.Parts {
			if text, ok := part.(TextContent); ok {
				b.text = append(b.text, newStreamPart(i, text.Text, text.Origin))
			}
		}
		if len(b.text) == 0 {
			b.text = []*streamPart{newStreamPart(len(b.message.Parts), "", "")}
			b.message.Parts = append(b.message.Parts, TextContent{})
		}
	}
	for _, part := range b.text {
		part.value.WriteString(delta)
	}
}

func (b *StreamBuffer) AppendReasoning(delta string) {
	if b.reasoning == nil {
		for i, part := range b.message.Parts {
			if text, ok := part.(ReasoningContent); ok {
				b.reasoning = append(b.reasoning, newStreamPart(i, text.Thinking, ""))
			}
		}
		if len(b.reasoning) == 0 {
			b.reasoning = []*streamPart{newStreamPart(len(b.message.Parts), "", "")}
			b.message.Parts = append(b.message.Parts, ReasoningContent{})
		}
	}
	for _, part := range b.reasoning {
		part.value.WriteString(delta)
	}
}

func newStreamPart(index int, initial, origin string) *streamPart {
	part := &streamPart{index: index, origin: origin}
	part.value.WriteString(initial)
	return part
}

func (b *StreamBuffer) Materialize() {
	for _, part := range b.text {
		text := b.message.Parts[part.index].(TextContent)
		text.Text, text.Origin = part.value.String(), part.origin
		b.message.Parts[part.index] = text
	}
	for _, part := range b.reasoning {
		b.message.Parts[part.index] = ReasoningContent{Thinking: part.value.String()}
	}
}
