package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type memoryMessages struct {
	message.Service
	mu    sync.Mutex
	items []message.Message
}

func (m *memoryMessages) Create(_ context.Context, owner string, p message.CreateMessageParams) (message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := message.Message{ID: fmt.Sprintf("message-%d", len(m.items)), SessionID: owner, Role: p.Role, Parts: append([]message.ContentPart(nil), p.Parts...), Model: p.Model}
	m.items = append(m.items, msg)
	return msg, nil
}
func (m *memoryMessages) Update(_ context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg.Parts = append([]message.ContentPart(nil), msg.Parts...)
	for i := range m.items {
		if m.items[i].ID == msg.ID {
			m.items[i] = msg
			return nil
		}
	}
	return fmt.Errorf("message missing")
}
func (m *memoryMessages) List(_ context.Context, owner string) ([]message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []message.Message
	for _, msg := range m.items {
		if msg.SessionID == owner {
			msg.Parts = append([]message.ContentPart(nil), msg.Parts...)
			result = append(result, msg)
		}
	}
	return result, nil
}

type memorySessions struct{ session.Service }

func (*memorySessions) Get(_ context.Context, id string) (session.Session, error) {
	return session.Session{ID: id}, nil
}
func (*memorySessions) Save(_ context.Context, s session.Session) (session.Session, error) {
	return s, nil
}

type generationCall struct {
	history []message.Message
	exposed []tools.BaseTool
	reply   chan provider.ProviderResponse
}
type controlledProvider struct{ calls chan generationCall }

func (*controlledProvider) Model() models.Model {
	return models.Model{ID: "test-model", Provider: models.ProviderOpenAI}
}
func (*controlledProvider) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	panic("unexpected synchronous request")
}
func (p *controlledProvider) StreamResponse(ctx context.Context, history []message.Message, ts []tools.BaseTool) <-chan provider.ProviderEvent {
	out := make(chan provider.ProviderEvent, 3)
	call := generationCall{exposed: append([]tools.BaseTool(nil), ts...), history: append([]message.Message(nil), history...), reply: make(chan provider.ProviderResponse, 1)}
	go func() {
		defer close(out)
		select {
		case p.calls <- call:
		case <-ctx.Done():
			out <- provider.ProviderEvent{Type: provider.EventError, Error: ctx.Err()}
			return
		}
		select {
		case r := <-call.reply:
			if r.Content != "" {
				out <- provider.ProviderEvent{Type: provider.EventContentDelta, Content: r.Content}
			}
			out <- provider.ProviderEvent{Type: provider.EventComplete, Response: &r}
		case <-ctx.Done():
			out <- provider.ProviderEvent{Type: provider.EventError, Error: ctx.Err()}
		}
	}()
	return out
}

type pausedTool struct {
	started, release chan struct{}
	calls            atomic.Int32
}

func (*pausedTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: "paused", Parameters: map[string]any{}}
}
func (p *pausedTool) Run(ctx context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	p.calls.Add(1)
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
		return tools.NewTextResponse("operation completed"), nil
	case <-ctx.Done():
		return tools.ToolResponse{}, ctx.Err()
	}
}

func steeringAgent(t *testing.T) (*agent, *controlledProvider, *memoryMessages) {
	t.Helper()
	if config.Get() == nil {
		dir := t.TempDir()
		path := filepath.Join(dir, ".opcode.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"openai":{"apiKey":"unit-test-key"}}}`), 0600))
		viper.SetConfigFile(path)
		_, err := config.Load(dir, false)
		require.NoError(t, err)
	}
	p := &controlledProvider{calls: make(chan generationCall, 8)}
	m := &memoryMessages{}
	a := &agent{Broker: pubsub.NewBroker[AgentEvent](), provider: p, messages: m, sessions: &memorySessions{}}
	t.Cleanup(func() { _ = a.Close() })
	return a, p, m
}
func nextGeneration(t *testing.T, p *controlledProvider) generationCall {
	t.Helper()
	select {
	case c := <-p.calls:
		return c
	case <-time.After(3 * time.Second):
		t.Fatal("provider call did not arrive")
		return generationCall{}
	}
}
func finishGeneration(t *testing.T, events <-chan AgentEvent) AgentEvent {
	t.Helper()
	select {
	case result := <-events:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not finish")
		return AgentEvent{}
	}
}

func TestInstructionsApplyAfterToolResultsAndSkipRemainingOldCalls(t *testing.T) {
	a, p, m := steeringAgent(t)
	tool := &pausedTool{started: make(chan struct{}, 1), release: make(chan struct{})}
	a.tools = []tools.BaseTool{tool}
	events, err := a.Run(context.Background(), "session", "original task")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	first.reply <- provider.ProviderResponse{FinishReason: message.FinishReasonToolUse, ToolCalls: []message.ToolCall{
		{ID: "one", Name: "paused", Input: `{}`, Finished: true}, {ID: "two", Name: "paused", Input: `{}`, Finished: true},
	}}
	select {
	case <-tool.started:
	case <-time.After(3 * time.Second):
		t.Fatal("tool did not start")
	}
	for _, text := range []string{"change direction", "also preserve my files"} {
		_, queued, err := a.Submit(context.Background(), "session", text)
		require.NoError(t, err)
		require.True(t, queued)
	}
	stored, _ := m.List(context.Background(), "session")
	for _, msg := range stored {
		require.NotEqual(t, "change direction", msg.Content().String(), "pending instructions are persisted only after tool results")
	}
	close(tool.release)
	second := nextGeneration(t, p)
	require.EqualValues(t, 1, tool.calls.Load(), "remaining old tool calls must be skipped")
	require.Len(t, second.history, 5)
	require.Equal(t, message.Tool, second.history[2].Role)
	require.Len(t, second.history[2].ToolResults(), 2)
	require.Contains(t, second.history[2].ToolResults()[1].Content, "new instructions")
	require.Equal(t, "change direction", second.history[3].Content().String())
	require.Equal(t, "also preserve my files", second.history[4].Content().String())
	second.reply <- provider.ProviderResponse{Content: "updated result", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, events)
	require.NoError(t, result.Error)
	require.Equal(t, "updated result", result.Message.Content().String())
}

func TestInstructionsDuringModelResponseAndSessionIsolation(t *testing.T) {
	a, p, _ := steeringAgent(t)
	events, err := a.Run(context.Background(), "session", "first task")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	_, queued, err := a.Submit(context.Background(), "session", "new constraints")
	require.NoError(t, err)
	require.True(t, queued)
	otherEvents, queued, err := a.Submit(context.Background(), "other", "independent task")
	require.NoError(t, err)
	require.False(t, queued)
	other := nextGeneration(t, p)
	require.Equal(t, "independent task", other.history[0].Content().String())
	other.reply <- provider.ProviderResponse{Content: "other result", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, otherEvents).Error)
	first.reply <- provider.ProviderResponse{Content: "initial response", FinishReason: message.FinishReasonEndTurn}
	second := nextGeneration(t, p)
	require.Equal(t, "new constraints", second.history[len(second.history)-1].Content().String())
	second.reply <- provider.ProviderResponse{Content: "revised response", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, events).Error)
}

func TestInstructionQueueIsBoundedAndCancellationClearsIt(t *testing.T) {
	a, p, m := steeringAgent(t)
	events, err := a.Run(context.Background(), "session", "task")
	require.NoError(t, err)
	_ = nextGeneration(t, p)
	for i := 0; i < 8; i++ {
		_, queued, err := a.Submit(context.Background(), "session", fmt.Sprintf("queued %d", i))
		require.NoError(t, err)
		require.True(t, queued)
	}
	_, _, err = a.Submit(context.Background(), "session", "overflow")
	require.ErrorContains(t, err, "queue is full")
	_, err = a.Run(context.Background(), "session", "concurrent run")
	require.ErrorIs(t, err, ErrSessionBusy)
	a.Cancel("session")
	require.ErrorIs(t, finishGeneration(t, events).Error, ErrRequestCancelled)
	items, _ := m.List(context.Background(), "session")
	for _, msg := range items {
		require.False(t, strings.HasPrefix(msg.Content().String(), "queued "))
	}
	require.False(t, a.IsSessionBusy("session"))
}
