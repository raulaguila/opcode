package chat

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"time"
)

type messageReconciliation struct {
	generation, version uint64
	pending, needed     bool
	cancel              context.CancelFunc
}
type messageReloadMsg struct {
	sessionID           string
	generation, version uint64
	messages            []message.Message
	err                 error
}

func (m *messagesCmp) resetMessageReconciliation() {
	if m.reconcile.cancel != nil {
		m.reconcile.cancel()
	}
	m.reconcile = messageReconciliation{generation: m.reconcile.generation + 1}
}
func (m *messagesCmp) reconcileMessageEvent(event pubsub.Event[message.Message]) tea.Cmd {
	if event.ReconcileRequired {
		m.reconcile.needed = true
	}
	if !m.reconcile.needed {
		return nil
	}
	return m.requestMessageReload()
}
func (m *messagesCmp) requestMessageReload() tea.Cmd {
	if m.reconcile.pending || m.app == nil || m.app.Messages == nil || m.session.ID == "" {
		return nil
	}
	m.reconcile.pending = true
	m.reconcile.generation++
	generation, version, id, service := m.reconcile.generation, m.reconcile.version, m.session.ID, m.app.Messages
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	m.reconcile.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		messages, err := service.List(ctx, id)
		return messageReloadMsg{id, generation, version, messages, err}
	}
}
func (m *messagesCmp) acceptMessageReload(result messageReloadMsg) tea.Cmd {
	if result.sessionID != m.session.ID || result.generation != m.reconcile.generation {
		return nil
	}
	m.reconcile.pending = false
	if result.version != m.reconcile.version {
		return m.requestMessageReload()
	}
	if result.err != nil {
		return nil
	} // Retry on the next message; never spin on an unavailable store.
	m.reconcile.needed = false
	m.messages = result.messages
	m.currentMsgID = ""
	if len(m.messages) > 0 {
		m.currentMsgID = m.messages[len(m.messages)-1].ID
	}
	offset, follow := m.viewport.YOffset, m.viewport.AtBottom() && !m.selection.engaged() && !m.find.open
	m.rerender()
	if follow {
		m.viewport.GotoBottom()
	} else {
		m.viewport.SetYOffset(offset)
	}
	return m.requestNestedHistory()
}
func (m *messagesCmp) nestedMessageEvent(event pubsub.Event[message.Message]) tea.Cmd {
	if event.Payload.SessionID != m.session.ID {
		if m.nested.tracked[event.Payload.SessionID] {
			return m.requestNestedHistory()
		}
		return nil
	}
	for _, id := range taskHistoryIDs([]message.Message{event.Payload}) {
		if !m.nested.tracked[id] {
			return m.requestNestedHistory()
		}
	}
	return nil
}
