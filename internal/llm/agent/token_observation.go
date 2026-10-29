package agent

import (
	"fmt"
	"math"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
)

type tokenObservation struct {
	Samples      int
	MaximumRatio float64
	Actual       int64
}

func observationKey(m models.Model) string {
	return fmt.Sprintf("%s/%s/%s/%s", m.Provider, m.ProviderProfile, m.API, m.APIModel)
}
func (a *agent) observeInputUsage(session string, m models.Model, u provider.TokenUsage) {
	actual := u.InputTokens + u.CacheCreationTokens + u.CacheReadTokens
	if actual <= 0 {
		return
	}
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	status, ok := a.contextStatuses[session]
	if !ok || status.EstimatedInputTokens <= 0 {
		return
	}
	if a.tokenObservations == nil {
		a.tokenObservations = map[string]tokenObservation{}
	}
	key := observationKey(m)
	obs := a.tokenObservations[key]
	// Ratios are measured against the uncalibrated estimate, not recursively.
	base := status.uncalibratedInput
	if base <= 0 {
		base = status.EstimatedInputTokens
	}
	ratio := float64(actual) / float64(max(1, base))
	obs.MaximumRatio = max(obs.MaximumRatio, ratio)
	obs.Samples++
	obs.Actual = actual
	a.tokenObservations[key] = obs
	status.ActualInputTokens = actual
	status.measuredForRequest = true
	status.CalibrationSamples = obs.Samples
	a.contextStatuses[session] = status
}
func (a *agent) calibratedEstimate(m models.Model, status ContextStatus) ContextStatus {
	a.contextMu.Lock()
	obs := a.tokenObservations[observationKey(m)]
	a.contextMu.Unlock()
	status.ActualInputTokens = obs.Actual
	status.CalibrationSamples = obs.Samples
	if obs.Samples >= 3 {
		factor := max(.35, obs.MaximumRatio*1.2)
		status.EstimatedInputTokens = int64(math.Ceil(float64(status.EstimatedInputTokens) * factor))
		status.Method += "; calibrated against provider usage with 20% margin, remains an estimate"
	}
	if status.ContextSource == "" {
		status.ContextSource = "model metadata (estimated when unavailable)"
	}
	status.Fits = status.EstimatedInputTokens+status.ReservedOutputTokens+status.SafetyTokens <= status.ContextWindow
	return status
}
