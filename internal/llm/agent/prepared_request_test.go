package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type preparedAgentProvider struct {
	provider.Provider
	requests []*preparedAgentRequest
}

func (*preparedAgentProvider) Model() models.Model {
	return models.Model{ID: "prepared-test", ContextWindow: 16384, DefaultMaxTokens: 2048}
}

func (p *preparedAgentProvider) PrepareRequest(history []message.Message, _ []tools.BaseTool, stream bool) (provider.PreparedRequest, error) {
	if !stream {
		return nil, errors.New("expected streaming preparation")
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return nil, err
	}
	request := &preparedAgentRequest{raw: raw}
	p.requests = append(p.requests, request)
	return request, nil
}

func (*preparedAgentProvider) StreamResponse(context.Context, []message.Message, []tools.BaseTool) <-chan provider.ProviderEvent {
	panic("agent rebuilt an already budgeted request")
}

type preparedAgentRequest struct {
	provider.PreparedRequest
	raw  []byte
	sent int
}

func (p *preparedAgentRequest) RequestFootprint() (provider.RequestFootprint, error) {
	return provider.RequestFootprint{ASCIIBytes: int64(len(p.raw)), ReservedOutputTokens: 2048, ExactSerialization: true}, nil
}

func (p *preparedAgentRequest) StreamResponse(context.Context) <-chan provider.ProviderEvent {
	p.sent++
	events := make(chan provider.ProviderEvent, 2)
	events <- provider.ProviderEvent{Type: provider.EventContentDelta, Content: "Hello."}
	events <- provider.ProviderEvent{Type: provider.EventComplete, Response: &provider.ProviderResponse{Content: "Hello.", FinishReason: message.FinishReasonEndTurn}}
	close(events)
	return events
}

func TestPreparedBudgetRetainsOnlyEnvelopeForCompactedHistory(t *testing.T) {
	p := &preparedAgentProvider{}
	a := &agent{provider: p}
	run := &generationRun{provider: p, mode: workflow.Ask}
	history := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Earlier question"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("x", 100000)}}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Current question stays intact"}}},
	}
	bounded, prepared, err := a.budgetPreparedRequest("s", history, run)
	require.NoError(t, err)
	require.Len(t, p.requests, 2)
	require.Same(t, p.requests[1], prepared)
	require.Zero(t, p.requests[0].sent)
	raw, err := json.Marshal(bounded)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(p.requests[1].raw))
	require.Equal(t, "Current question stays intact", bounded[2].Content().Text)
	adapter := &preparedStreamProvider{Provider: p, prepared: prepared}
	for range timedProvider(adapter).StreamResponse(context.Background(), history, nil) {
	}
	require.Equal(t, 1, p.requests[1].sent)
	require.Zero(t, p.requests[0].sent)
}

func TestPreparedEnvelopeIsLocalToEachGeneration(t *testing.T) {
	a, _, _ := steeringAgent(t)
	p := &preparedAgentProvider{}
	a.provider = p
	for _, text := range []string{"Hello", "Thanks"} {
		done, err := a.Run(context.Background(), "session", text)
		require.NoError(t, err)
		require.NoError(t, finishGeneration(t, done).Error)
	}
	require.Len(t, p.requests, 2)
	require.Equal(t, 1, p.requests[0].sent)
	require.Equal(t, 1, p.requests[1].sent)
	require.NotEqual(t, string(p.requests[0].raw), string(p.requests[1].raw))
}

func TestPreparedBudgetRejectsOversizedCurrentInputWithoutSending(t *testing.T) {
	p := &preparedAgentProvider{}
	a := &agent{provider: p}
	run := &generationRun{provider: p, mode: workflow.Ask}
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("x", 100000)}}}}
	_, prepared, err := a.budgetPreparedRequest("s", history, run)
	require.ErrorIs(t, err, ErrContextLimit)
	require.Nil(t, prepared)
	require.Len(t, p.requests, 1)
	require.Zero(t, p.requests[0].sent)
	require.Len(t, history[0].Content().Text, 100000)
}
