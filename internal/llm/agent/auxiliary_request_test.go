package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

type auxiliaryPreparedProbe struct {
	provider.Provider
	requests       []*auxiliaryPreparedEnvelope
	prepareError   error
	footprintError error
}

func (*auxiliaryPreparedProbe) Model() models.Model {
	return models.Model{ID: "prepared-auxiliary", ContextWindow: 16384, DefaultMaxTokens: 128}
}
func (*auxiliaryPreparedProbe) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	panic("auxiliary rebuilt an approved prepared request")
}
func (p *auxiliaryPreparedProbe) PrepareRequest(history []message.Message, definitions []tools.BaseTool, stream bool) (provider.PreparedRequest, error) {
	if stream {
		return nil, errors.New("auxiliary must prepare nonstreaming JSON")
	}
	if p.prepareError != nil {
		return nil, p.prepareError
	}
	var schema []tools.ToolInfo
	for _, tool := range definitions {
		schema = append(schema, tool.Info())
	}
	raw, err := json.Marshal(struct {
		History []message.Message
		Tools   []tools.ToolInfo
	}{history, schema})
	if err != nil {
		return nil, err
	}
	request := &auxiliaryPreparedEnvelope{raw: raw, footprintError: p.footprintError}
	p.requests = append(p.requests, request)
	return request, nil
}

type auxiliaryPreparedEnvelope struct {
	provider.PreparedRequest
	raw            []byte
	measured, sent int
	footprintError error
}

func (p *auxiliaryPreparedEnvelope) RequestFootprint() (provider.RequestFootprint, error) {
	p.measured++
	return provider.RequestFootprint{ASCIIBytes: int64(len(p.raw)), ReservedOutputTokens: 128, ExactSerialization: true}, p.footprintError
}
func (p *auxiliaryPreparedEnvelope) SendMessages(ctx context.Context) (*provider.ProviderResponse, error) {
	p.sent++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &provider.ProviderResponse{Content: "Prepared auxiliary"}, nil
}

func auxiliaryHistory(text string) []message.Message {
	return []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}}
}

func TestAuxiliaryRolesSendOnlyMeasuredNonstreamingEnvelope(t *testing.T) {
	steeringAgent(t) // Initialize the isolated configuration required by task prompts.
	for _, role := range []config.AgentName{config.AgentTitle, config.AgentSummarizer, config.AgentTask} {
		t.Run(string(role), func(t *testing.T) {
			p := &auxiliaryPreparedProbe{}
			response, err := sendAuxiliaryRequest(context.Background(), role, p, auxiliaryHistory("Explain the project"), nil)
			require.NoError(t, err)
			require.Equal(t, "Prepared auxiliary", response.Content)
			require.Len(t, p.requests, 1)
			require.Equal(t, 1, p.requests[0].measured)
			require.Equal(t, 1, p.requests[0].sent)
		})
	}
}

func TestAuxiliaryCompactionInvalidatesEarlierEnvelope(t *testing.T) {
	steeringAgent(t) // Initialize the isolated configuration required by task prompts.
	p := &auxiliaryPreparedProbe{}
	history := append(auxiliaryHistory("Earlier question"), message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("x", 100000)}}})
	history = append(history, auxiliaryHistory("Current question stays intact")...)
	_, err := sendAuxiliaryRequest(context.Background(), config.AgentSummarizer, p, history, nil)
	require.NoError(t, err)
	require.Len(t, p.requests, 2)
	require.Zero(t, p.requests[0].sent)
	require.Equal(t, 1, p.requests[1].sent)
	require.Contains(t, string(p.requests[1].raw), "Current question stays intact")
	require.NotEqual(t, string(p.requests[0].raw), string(p.requests[1].raw))
}

func TestAuxiliaryOversizedAndFailedCandidatesNeverSend(t *testing.T) {
	steeringAgent(t) // Initialize the isolated configuration required by task prompts.
	p := &auxiliaryPreparedProbe{}
	_, err := sendAuxiliaryRequest(context.Background(), config.AgentTitle, p, auxiliaryHistory(strings.Repeat("x", 100000)), nil)
	require.ErrorIs(t, err, ErrContextLimit)
	require.NotEmpty(t, p.requests)
	for _, request := range p.requests {
		require.Zero(t, request.sent)
	}
	failure := errors.New("preparation failed")
	p = &auxiliaryPreparedProbe{prepareError: failure}
	_, err = sendAuxiliaryRequest(context.Background(), config.AgentTask, p, auxiliaryHistory("Small input"), nil)
	require.ErrorIs(t, err, failure)
	require.Empty(t, p.requests)
	p = &auxiliaryPreparedProbe{footprintError: failure}
	_, err = sendAuxiliaryRequest(context.Background(), config.AgentTask, p, auxiliaryHistory("Small input"), nil)
	require.ErrorIs(t, err, failure)
	require.Len(t, p.requests, 1)
	require.Zero(t, p.requests[0].sent)
}

func TestAuxiliaryNewHistoryAndReadToolsPrepareNewEnvelopeWithoutExecutingReads(t *testing.T) {
	steeringAgent(t) // Initialize the isolated configuration required by task prompts.
	p := &auxiliaryPreparedProbe{}
	first := &countedTool{name: "view"}
	second := &countedTool{name: "read_lines"}
	_, err := sendAuxiliaryRequest(context.Background(), config.AgentTask, p, auxiliaryHistory("First question"), []tools.BaseTool{first})
	require.NoError(t, err)
	_, err = sendAuxiliaryRequest(context.Background(), config.AgentTask, p, auxiliaryHistory("Second question"), []tools.BaseTool{second})
	require.NoError(t, err)
	require.Len(t, p.requests, 2)
	require.Contains(t, string(p.requests[0].raw), "view")
	require.Contains(t, string(p.requests[1].raw), "read_lines")
	require.Contains(t, string(p.requests[1].raw), "Second question")
	require.Equal(t, 1, p.requests[0].sent)
	require.Equal(t, 1, p.requests[1].sent)
	require.Zero(t, first.count.Load())
	require.Zero(t, second.count.Load())
}

func TestAuxiliaryPreparedRequestPreservesCancellation(t *testing.T) {
	steeringAgent(t) // Initialize the isolated configuration required by task prompts.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := sendAuxiliaryRequest(ctx, config.AgentSummarizer, &auxiliaryPreparedProbe{}, auxiliaryHistory("Small input"), nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTitleUsesPreparedEnvelopeWhenSeparatelySelected(t *testing.T) {
	steeringAgent(t) // Initialize the isolated configuration required by task prompts.
	p := &auxiliaryPreparedProbe{}
	sessions := &titleSessions{}
	a := &agent{provider: &titleProbe{model: models.Model{ID: "main", Provider: models.ProviderLocal}}, titleProvider: p, sessions: sessions}
	require.NoError(t, a.generateTitle(context.Background(), "s", "Explain the project"))
	require.Equal(t, "Prepared auxiliary", sessions.saved.Title)
	require.Len(t, p.requests, 1)
	require.Equal(t, 1, p.requests[0].sent)
}
