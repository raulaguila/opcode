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
		return agent.UsageMetrics{ActiveModel: p.metrics.ActiveModel, ActiveContextWindow: p.metrics.ActiveContextWindow, ActiveContextSource: p.metrics.ActiveContextSource}, nil
	}
	return p.metrics, nil
}

func TestFooterUsesActiveProviderContextEvenBeforeFirstMessage(t *testing.T) {
	ensureSetupTestConfig(t)
	p := workflowPage(t)
	cfg := config.Get()
	old := cfg.Agents
	t.Cleanup(func() { cfg.Agents = old })
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentCoder: {Model: models.GPT41}}
	p.session.ID = ""
	p.app.CoderAgent = &footerProbe{workflowProbe: workflowProbe{store: p.app.Workflows}, metrics: agent.UsageMetrics{ActiveModel: models.GPT41, ActiveContextWindow: 4096, ActiveContextSource: "learned after provider context/memory failure"}}
	state := p.FooterState()
	require.EqualValues(t, 4096, state.ContextWindow, "initial footer must match the bound provider, not its advertised capacity")
	require.False(t, state.ContextKnown, "no token usage exists before a request")
	require.Zero(t, state.TokensPerSecond)
	cfg.Agents[config.AgentCoder] = config.Agent{Model: models.GPT4o}
	require.NotEqualValues(t, 4096, p.FooterState().ContextWindow, "ignore active metadata for a different model")
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

func TestFooterShowsCurrentStreamWithoutReusingPreviousSpeed(t *testing.T) {
	ensureSetupTestConfig(t)
	p := workflowPage(t)
	cfg := config.Get()
	old := cfg.Agents
	t.Cleanup(func() { cfg.Agents = old })
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentCoder: {Model: models.GPT41}}
	probe := &footerProbe{workflowProbe: workflowProbe{store: p.app.Workflows, busy: true}, metrics: agent.UsageMetrics{
		LastModel: models.GPT41, LastOutputTokens: 50, LastOutputDurationNS: 2_000_000_000,
		StreamModel: models.GPT41, StreamActive: true,
	}}
	p.app.CoderAgent = probe
	state := p.FooterState()
	require.True(t, state.AwaitingModel)
	require.Zero(t, state.TokensPerSecond)
	probe.metrics.StreamOutputBytes, probe.metrics.StreamDurationNS = 80, 2_000_000_000
	state = p.FooterState()
	require.False(t, state.AwaitingModel)
	require.True(t, state.SpeedLive)
	require.True(t, state.SpeedEstimated)
	require.Equal(t, 10.0, state.TokensPerSecond)
	probe.metrics.StreamDurationNS = 1
	require.Zero(t, p.FooterState().TokensPerSecond, "suppress first-packet spike")
	probe.metrics.StreamDurationNS = 2_000_000_000
	cfg.Agents[config.AgentCoder] = config.Agent{Model: models.GPT4o}
	require.Zero(t, p.FooterState().TokensPerSecond, "stream speed cannot leak across models")
	cfg.Agents[config.AgentCoder] = config.Agent{Model: models.GPT41}
	p.session.ID = "other"
	require.Zero(t, p.FooterState().TokensPerSecond, "stream speed cannot leak across sessions")
	p.session.ID = "s"
	probe.metrics.StreamActive = false
	require.Zero(t, p.FooterState().TokensPerSecond, "tools or approvals must not display an earlier call's rate")
	probe.busy = false
	state = p.FooterState()
	require.Equal(t, 25.0, state.TokensPerSecond)
	require.False(t, state.SpeedLive)
}
