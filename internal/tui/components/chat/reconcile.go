package chat

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/tui/util"
	"time"
)

type messageReconciliation struct {
	generation, version  uint64
	pending, needed, gap bool
	cancel               context.CancelFunc
	journal              []journalEvent
	bytes                int
}
type messageReloadMsg struct {
	sessionID           string
	generation, version uint64
	messages            []message.Message
	err                 error
	page                *message.HistoryPage
	older               bool
}

func (m *messagesCmp) resetMessageReconciliation() {
	if m.reconcile.cancel != nil {
		m.reconcile.cancel()
	}
	m.reconcile = messageReconciliation{generation: m.reconcile.generation + 1}
	m.loadingPreview = loadingPreview{}
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
func (m *messagesCmp) requestMessageReload() tea.Cmd { return m.requestHistoryRead(false) }

func (m *messagesCmp) requestHistoryRead(older bool) tea.Cmd {
	if m.reconcile.pending || m.app == nil || m.app.Messages == nil || m.session.ID == "" {
		return nil
	}
	m.reconcile.pending = true
	m.reconcile.generation++
	m.reconcile.journal, m.reconcile.bytes, m.reconcile.gap = nil, 0, false
	generation, version, id, service := m.reconcile.generation, m.reconcile.version, m.session.ID, m.app.Messages
	before, all := m.history.before, m.history.all || m.history.paged && !m.history.more
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	m.reconcile.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		var page message.HistoryPage
		var paged bool
		var err error
		if older {
			paged = true
			page, err = service.(message.HistoryPager).ListPage(ctx, id, before, transcriptPageMessages)
			if err == nil {
				err = validateOlderPage(before, page)
			}
		} else {
			page, paged, err = readTranscriptHistory(ctx, service, id, before, all)
		}
		result := messageReloadMsg{sessionID: id, generation: generation, version: version, messages: page.Messages, err: err, older: older}
		if paged {
			result.page = &page
		}
		return result
	}
}
func (m *messagesCmp) acceptMessageReload(result messageReloadMsg) tea.Cmd {
	if result.sessionID != m.session.ID || result.generation != m.reconcile.generation {
		return nil
	}
	m.reconcile.pending = false
	if result.err != nil {
		m.rendering = false
		m.loadingPreview = loadingPreview{}
		m.reconcile.journal = nil
		return util.ReportError(result.err)
	}
	messages := result.messages
	if result.older {
		messages = prependHistory(messages, m.messages)
	}
	messages, complete := m.reconcileSnapshot(messages, result.version)
	if !complete {
		m.reconcile.needed = true
		return m.requestMessageReload()
	}
	m.reconcile.journal, m.reconcile.bytes = nil, 0
	return m.installHistorySnapshot(result, messages)
}

func (m *messagesCmp) installHistorySnapshot(result messageReloadMsg, messages []message.Message) tea.Cmd {
	m.loadingPreview = loadingPreview{}
	initial := m.rendering
	m.rendering = false
	m.reconcile.needed = false
	m.messages = messages
	if result.page != nil {
		m.history.before, m.history.more, m.history.paged = result.page.Before, result.page.More, true
	}
	m.currentMsgID = ""
	if len(m.messages) > 0 {
		m.currentMsgID = m.messages[len(m.messages)-1].ID
	}
	offset, lines := m.viewport.YOffset, len(m.transcriptLines)
	follow := !result.older && (initial || m.viewport.AtBottom()) && !m.selection.engaged() && !m.find.open
	if result.older {
		m.history.first = ""
	}
	m.rerender()
	if follow {
		m.viewport.GotoBottom()
	} else {
		if result.older {
			offset += len(m.transcriptLines) - lines
		}
		m.viewport.SetYOffset(offset)
	}
	return tea.Batch(m.continueHistoryRead(), m.requestNestedHistory())
}

func prependHistory(older, current []message.Message) []message.Message {
	seen := make(map[string]bool, len(current))
	for _, item := range current {
		seen[item.ID] = true
	}
	result := make([]message.Message, 0, len(older)+len(current))
	for _, item := range older {
		if !seen[item.ID] {
			result = append(result, item)
		}
	}
	return append(result, current...)
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
