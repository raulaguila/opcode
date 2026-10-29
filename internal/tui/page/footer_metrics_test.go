package page

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

type footerProbe struct {
	workflowProbe
	metrics agent.UsageMetrics
}

func (p *footerProbe) Usage(_ context.Context, id string) (agent.UsageMetrics, error) {
	if id != "s" {
		return agent.UsageMetrics{}, nil
	}
	return p.metrics, nil
}

func TestFooterUsesEffectiveCachedContextAndSessionModelScopedSpeed(t *testing.T) {
	ensureSetupTestConfig(t)
	p := workflowPage(t)
	cfg := config.Get()
	old := cfg.Agents
	t.Cleanup(func() { cfg.Agents = old })
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentCoder: {Model: models.GPT41}}
	p.app.CoderAgent = &footerProbe{workflowProbe: workflowProbe{store: p.app.Workflows}, metrics: agent.UsageMetrics{LastModel: models.GPT41, LastOutputTokens: 50, LastOutputDurationNS: 2_000_000_000, LastDurationSource: "provider", ContextWindow: 16384, ContextTokens: 4000}}
	state := p.FooterState()
	require.EqualValues(t, 16384, state.ContextWindow)
	require.EqualValues(t, 4000, state.ContextTokens)
	require.True(t, state.ContextKnown)
	require.Equal(t, 25.0, state.TokensPerSecond)
	cfg.Agents[config.AgentCoder] = config.Agent{Model: models.GPT4o}
	require.Zero(t, p.FooterState().TokensPerSecond, "do not carry speed across models")
	p.session.ID = "other"
	require.False(t, p.FooterState().ContextKnown)
	require.Zero(t, p.FooterState().TokensPerSecond)
}
