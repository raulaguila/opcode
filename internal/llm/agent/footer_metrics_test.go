package agent

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/stretchr/testify/require"
)

func TestFooterUsageDoesNotLabelOldActualCountAsCurrentMeasurement(t *testing.T) {
	a := &agent{contextStatuses: map[string]ContextStatus{"s": {ContextWindow: 32768, EstimatedInputTokens: 2000, ActualInputTokens: 1000}}}
	a.observeUsage("s", provider.TokenUsage{OutputTokens: 25, OutputDurationNS: 1_000_000_000, DurationSource: "provider"}, models.ModelID("fixture"))
	metrics, err := a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.EqualValues(t, 2000, metrics.ContextTokens)
	require.True(t, metrics.ContextEstimated)
	a.observeInputUsage("s", models.Model{ID: "fixture"}, provider.TokenUsage{InputTokens: 1800})
	metrics, err = a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.EqualValues(t, 1800, metrics.ContextTokens)
	require.False(t, metrics.ContextEstimated)
	require.EqualValues(t, 25, metrics.LastOutputTokens)
	require.EqualValues(t, 1_000_000_000, metrics.LastOutputDurationNS)
	other, err := a.Usage(context.Background(), "other")
	require.NoError(t, err)
	require.Zero(t, other)
}
