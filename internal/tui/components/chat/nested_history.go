package chat

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
)

const nestedHistoryLimit = 2 << 20
const nestedTaskLimit = 32

type nestedHistory struct {
	snapshots      map[string][]message.Message
	tracked        map[string]bool
	pending, again bool
	generation     uint64
	cancel         context.CancelFunc
}
type nestedHistoryMsg struct {
	sessionID  string
	generation uint64
	snapshots  map[string][]message.Message
}

// Renderers receive a bounded in-memory projection, never the persistence
// service. The per-frame visit budget also terminates malformed cyclic trees.
type nestedMessageProjection struct {
	snapshots map[string][]message.Message
	visited   map[string]bool
	remaining int
}

func (p *nestedMessageProjection) List(_ context.Context, id string) ([]message.Message, error) {
	if p.remaining <= 0 || p.visited[id] {
		return nil, nil
	}
	p.remaining--
	p.visited[id] = true
	return p.snapshots[id], nil
}
func (m *messagesCmp) nestedProjection() taskHistoryReader {
	return &nestedMessageProjection{snapshots: m.nested.snapshots, visited: map[string]bool{}, remaining: nestedTaskLimit}
}
func (m *messagesCmp) resetNestedHistory() {
	if m.nested.cancel != nil {
		m.nested.cancel()
	}
	m.nested = nestedHistory{generation: m.nested.generation + 1}
}
func taskHistoryIDs(messages []message.Message) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, msg := range messages {
		for _, call := range msg.ToolCalls() {
			if call.Name == agent.AgentToolName && !seen[call.ID] {
				seen[call.ID] = true
				ids = append(ids, call.ID)
			}
		}
	}
	return ids
}
func (m *messagesCmp) requestNestedHistory() tea.Cmd {
	ids := taskHistoryIDs(m.messages)
	if len(ids) == 0 || m.app == nil || m.app.Messages == nil {
		return nil
	}
	if m.nested.pending {
		m.nested.again = true
		return nil
	}
	if m.nested.tracked == nil {
		m.nested.tracked = map[string]bool{}
	}
	for _, id := range ids {
		m.nested.tracked[id] = true
	}
	m.nested.pending = true
	m.nested.generation++
	generation, sessionID, service := m.nested.generation, m.session.ID, m.app.Messages
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	m.nested.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		return nestedHistoryMsg{sessionID: sessionID, generation: generation, snapshots: loadNestedHistory(ctx, service, ids)}
	}
}
func loadNestedHistory(ctx context.Context, service message.Service, ids []string) map[string][]message.Message {
	snapshots := map[string][]message.Message{}
	total := 0
	for len(ids) > 0 && len(snapshots) < nestedTaskLimit && ctx.Err() == nil {
		id := ids[0]
		ids = ids[1:]
		if _, seen := snapshots[id]; seen {
			continue
		}
		history, err := service.List(ctx, id)
		if err != nil {
			snapshots[id] = nil
			continue
		}
		history = compactTaskHistory(history)
		size := messageHistoryBytes(history)
		if total+size > nestedHistoryLimit {
			break
		}
		total += size
		snapshots[id] = history
		ids = append(ids, taskHistoryIDs(history)...)
	}
	return snapshots
}
func messageHistoryBytes(history []message.Message) int {
	size := len(history) * 128
	for _, msg := range history {
		size += len(msg.Content().Text) + len(msg.ReasoningContent().Thinking)
		for _, call := range msg.ToolCalls() {
			size += len(call.Input) + len(call.Name) + len(call.ID)
		}
		for _, result := range msg.ToolResults() {
			size += len(result.Content) + len(result.Metadata) + len(result.ToolCallID)
		}
	}
	return size
}
func (m *messagesCmp) acceptNestedHistory(msg nestedHistoryMsg) tea.Cmd {
	if msg.sessionID != m.session.ID || msg.generation != m.nested.generation {
		return nil
	}
	m.nested.pending = false
	if m.nested.again {
		m.nested.again = false
		return m.requestNestedHistory()
	}
	m.nested.snapshots = msg.snapshots
	m.nested.tracked = map[string]bool{}
	for _, id := range taskHistoryIDs(m.messages) {
		m.nested.tracked[id] = true
	}
	m.invalidateProcessCaches()
	for id := range msg.snapshots {
		m.nested.tracked[id] = true
	}
	m.renderCache.dirty = true
	for _, group := range m.renderCache.groups {
		m.renderCache.versions[group.key]++
	}
	m.renderView()
	return nil
}

func compactTaskHistory(history []message.Message) []message.Message {
	result := []message.Message{}
	for _, msg := range history {
		calls := msg.ToolCalls()
		if len(calls) == 0 {
			continue
		}
		copy := message.Message{ID: msg.ID, SessionID: msg.SessionID, Role: msg.Role}
		for _, call := range calls {
			copy.Parts = append(copy.Parts, call)
		}
		result = append(result, copy)
	}
	return result
}
