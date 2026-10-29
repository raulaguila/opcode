package app

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/stretchr/testify/require"
)

type usageDelegate struct{ agent.Service }

func (*usageDelegate) Usage(_ context.Context, id string) (agent.UsageMetrics, error) {
	if id == "active" {
		return agent.UsageMetrics{LastOutputTokens: 42}, nil
	}
	return agent.UsageMetrics{}, nil
}

func TestConfigurableAgentForwardsUsageToActiveDelegate(t *testing.T) {
	p := &configurableCoder{delegate: &usageDelegate{}}
	m, err := p.Usage(context.Background(), "active")
	require.NoError(t, err)
	require.EqualValues(t, 42, m.LastOutputTokens)
	m, err = p.Usage(context.Background(), "other")
	require.NoError(t, err)
	require.Zero(t, m)
	p.delegate = nil
	_, err = p.Usage(context.Background(), "active")
	require.Error(t, err)
}
