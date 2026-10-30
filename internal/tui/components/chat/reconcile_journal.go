package chat

import (
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"slices"
)

const messageJournalEvents = 128
const messageJournalBytes = 2 << 20

type journalEvent struct {
	version uint64
	event   pubsub.Event[message.Message]
}

func (m *messagesCmp) journalMessageEvent(event pubsub.Event[message.Message]) {
	if event.Payload.SessionID != m.session.ID && !event.ReconcileRequired {
		return
	}
	m.reconcile.version++
	if !m.reconcile.pending || m.reconcile.gap {
		return
	}
	size := historyEventBytes(event.Payload)
	if event.ReconcileRequired || len(m.reconcile.journal) >= messageJournalEvents || m.reconcile.bytes+size > messageJournalBytes {
		m.reconcile.gap = true
		m.reconcile.journal = nil
		m.reconcile.bytes = 0
		return
	}
	event.Payload = message.HistorySnapshot(event.Payload)
	m.reconcile.journal = append(m.reconcile.journal, journalEvent{m.reconcile.version, event})
	m.reconcile.bytes += size
}

func historyEventBytes(item message.Message) int {
	size := len(item.ID) + len(item.SessionID) + len(item.Parts)*128
	for _, part := range item.Parts {
		switch value := part.(type) {
		case message.TextContent:
			size += len(value.Text) + len(value.Origin)
		case message.ReasoningContent:
			size += len(value.Thinking)
		case message.ToolCall:
			size += len(value.ID) + len(value.Name) + len(value.Input)
		case message.ToolResult:
			size += len(value.ToolCallID) + len(value.Content) + len(value.Metadata)
		case message.BinaryContent:
			size += len(value.Path) + len(value.MIMEType)
		}
	}
	return size
}

func (m *messagesCmp) reconcileSnapshot(messages []message.Message, version uint64) ([]message.Message, bool) {
	if m.reconcile.gap {
		return nil, false
	}
	cloned := false
	for _, entry := range m.reconcile.journal {
		if entry.version <= version {
			continue
		}
		if entry.version != version+1 {
			return nil, false
		}
		if !cloned {
			messages = slices.Clone(messages)
			cloned = true
		}
		messages = applyHistoryEvent(messages, entry.event)
		version = entry.version
	}
	return messages, version == m.reconcile.version
}

func applyHistoryEvent(messages []message.Message, event pubsub.Event[message.Message]) []message.Message {
	for i := range messages {
		if messages[i].ID != event.Payload.ID {
			continue
		}
		if event.Type == pubsub.DeletedEvent {
			copy(messages[i:], messages[i+1:])
			clear(messages[len(messages)-1:])
			return messages[:len(messages)-1]
		}
		messages[i] = event.Payload
		return messages
	}
	if event.Type == pubsub.CreatedEvent {
		messages = append(messages, event.Payload)
	}
	return messages
}
