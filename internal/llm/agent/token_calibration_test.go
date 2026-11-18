package agent

import (
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/stretchr/testify/require"
)

// Live gemma4:12b: estimated ~9.4K input, Ollama measured 4271. Waiting for
// three samples in one process meant a fresh run never used the measurement.
func TestMeasuredTokenRatioIsUsedAtOnceAndByTheNextRun(t *testing.T) {
	steeringAgent(t) // loads a configuration
	cfg := config.Get()
	data := cfg.Data.Directory
	cfg.Data.Directory = t.TempDir()
	t.Cleanup(func() { cfg.Data.Directory = data })
	model := models.Model{Provider: models.ProviderOpenAI, APIModel: "ratio-fixture"}

	first := &agent{contextStatuses: map[string]ContextStatus{"s": {EstimatedInputTokens: 10000, uncalibratedInput: 10000}}}
	first.calibratedEstimate(model, ContextStatus{ContextWindow: 16384, EstimatedInputTokens: 10000})
	first.observeInputUsage("s", model, provider.TokenUsage{InputTokens: 4500})
	status := first.calibratedEstimate(model, ContextStatus{ContextWindow: 16384, EstimatedInputTokens: 10000})
	require.InDelta(t, 5850, status.EstimatedInputTokens, 1, "one sample: 0.45 with a 30% margin")
	require.Contains(t, status.Method, "30% margin")

	next := &agent{} // a new process
	status = next.calibratedEstimate(model, ContextStatus{ContextWindow: 16384, EstimatedInputTokens: 10000})
	require.InDelta(t, 5850, status.EstimatedInputTokens, 1, "the saved ratio applies before any request")

	require.NoError(t, saveContextCalibration(model, 0))
	status = (&agent{}).calibratedEstimate(model, ContextStatus{ContextWindow: 16384, EstimatedInputTokens: 10000})
	require.EqualValues(t, 10000, status.EstimatedInputTokens, "recalibrating forgets the ratio")
}

type limitRecorder struct {
	provider.Provider
	limit int64
}

func (l *limitRecorder) WithMaxTokens(limit int64) (provider.Provider, error) {
	return &limitRecorder{limit: limit}, nil
}
func (l *limitRecorder) WithSystemPrompt(string) (provider.Provider, error) { return l, nil }
func (l *limitRecorder) WithoutThinking() (provider.Provider, error)        { return l, nil }

func TestTightStepLowersTheReplyLimitOnlyWhenARealReplyStillFits(t *testing.T) {
	a := &agent{contextStatuses: map[string]ContextStatus{}}
	run := &generationRun{provider: &limitRecorder{}}
	a.contextStatuses["s"] = ContextStatus{ContextWindow: 16384, EstimatedInputTokens: 12000, SafetyTokens: 819, ReservedOutputTokens: 4096}
	require.True(t, a.shrinkOutput("s", run))
	require.EqualValues(t, 16384-12000-819, run.provider.(*limitRecorder).limit)
	require.EqualValues(t, 4096, run.fullOutput, "the full limit comes back next step")

	a.contextStatuses["s"] = ContextStatus{ContextWindow: 16384, EstimatedInputTokens: 15000, SafetyTokens: 819, ReservedOutputTokens: 4096}
	require.False(t, a.shrinkOutput("s", &generationRun{provider: &limitRecorder{}}), "under 1024 tokens of room the request is refused, not sent")
}
