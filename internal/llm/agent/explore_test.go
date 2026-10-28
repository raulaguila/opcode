package agent

import (
	"context"
	"encoding/json"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
	"testing"
)

type explorationProvider struct {
	provider.Provider
	send func(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error)
}

func (*explorationProvider) Model() models.Model {
	return models.Model{ID: "explore", ContextWindow: 64000, DefaultMaxTokens: 1024}
}
func (p *explorationProvider) SendMessages(ctx context.Context, h []message.Message, t []tools.BaseTool) (*provider.ProviderResponse, error) {
	return p.send(ctx, h, t)
}
func TestExploreIsSeparateReadOnlyAndUsesParentBudgets(t *testing.T) {
	read := &countedTool{name: "view", output: "actual source evidence"}
	calls := 0
	p := &explorationProvider{send: func(ctx context.Context, h []message.Message, ts []tools.BaseTool) (*provider.ProviderResponse, error) {
		calls++
		require.Len(t, ts, 1)
		require.Equal(t, "view", ts[0].Info().Name)
		if calls == 1 {
			require.Len(t, h, 1)
			return &provider.ProviderResponse{ToolCalls: []message.ToolCall{toolCall("view", `{}`), toolCall("bash", `{"command":"touch forbidden"}`), toolCall("explore_code", `{"question":"nested"}`)}}, nil
		}
		require.Contains(t, h[len(h)-1].ToolResults()[0].Content, "actual source evidence")
		require.True(t, h[len(h)-1].ToolResults()[1].IsError)
		require.True(t, h[len(h)-1].ToolResults()[2].IsError)
		return &provider.ProviderResponse{Content: "Found source.go:1"}, nil
	}}
	tool := &exploreTool{available: []tools.BaseTool{read}, create: func() (provider.Provider, error) { return p, nil }}
	run := &generationRun{limits: config.RuntimeConfig{MaxSteps: 4, ToolBudget: 4096}}
	ctx := context.WithValue(context.Background(), explorationRunKey{}, run)
	response, err := tool.Run(ctx, tools.ToolCall{Input: `{"question":"Locate the parser"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Contains(t, response.Content, "source.go:1")
	require.Equal(t, 2, run.steps)
	require.Positive(t, run.toolBytes)
	require.EqualValues(t, 1, read.count.Load())
	response, err = tool.Run(context.Background(), tools.ToolCall{Input: `{"question":"Locate parser"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "parent budget")
}
func TestExploreStopsAtSharedStepBudgetAndRejectsOversizedQuestion(t *testing.T) {
	read := &countedTool{name: "view", output: "result"}
	p := &explorationProvider{send: func(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
		return &provider.ProviderResponse{ToolCalls: []message.ToolCall{toolCall("view", `{}`)}}, nil
	}}
	tool := &exploreTool{available: []tools.BaseTool{read}, create: func() (provider.Provider, error) { return p, nil }}
	run := &generationRun{limits: config.RuntimeConfig{MaxSteps: 1, ToolBudget: 4096}}
	response, err := tool.Run(context.WithValue(context.Background(), explorationRunKey{}, run), tools.ToolCall{Input: `{"question":"Find it"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Equal(t, 1, run.steps)
	require.EqualValues(t, 1, read.count.Load())
	raw, _ := json.Marshal(map[string]string{"question": string(make([]byte, 4001))})
	response, err = tool.Run(context.Background(), tools.ToolCall{Input: string(raw)})
	require.NoError(t, err)
	require.True(t, response.IsError)
}
func TestExploreCancellationDoesNotRunReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run := &generationRun{limits: config.RuntimeConfig{MaxSteps: 5, ToolBudget: 4096}}
	read := &countedTool{name: "view"}
	tool := &exploreTool{available: []tools.BaseTool{read}, create: func() (provider.Provider, error) { return &explorationProvider{}, nil }}
	_, err := tool.Run(context.WithValue(ctx, explorationRunKey{}, run), tools.ToolCall{Input: `{"question":"Find it"}`})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, read.count.Load())
}
