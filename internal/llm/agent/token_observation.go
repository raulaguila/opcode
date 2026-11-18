package agent

import (
	"fmt"
	"math"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/logging"
)

type tokenObservation struct {
	Samples      int
	MaximumRatio float64
	Actual       int64
	// Saved is the ratio a previous run measured; loaded once per process.
	Saved  float64
	loaded bool
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
	status, ok := a.contextStatuses[session]
	if !ok || status.EstimatedInputTokens <= 0 {
		a.contextMu.Unlock()
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
	save := obs.MaximumRatio > obs.Saved*1.05 // write when it grows, not every step
	if save {
		obs.Saved = obs.MaximumRatio
		a.tokenObservations[key] = obs
	}
	a.contextMu.Unlock()
	if save {
		if err := saveTokenRatio(m, obs.MaximumRatio); err != nil {
			logging.Debug("Token calibration not saved", "error", err)
		}
	}
}
func (a *agent) calibratedEstimate(m models.Model, status ContextStatus) ContextStatus {
	key := observationKey(m)
	a.contextMu.Lock()
	obs := a.tokenObservations[key]
	a.contextMu.Unlock()
	if !obs.loaded {
		saved := learnedTokenRatio(m) // disk, outside the lock
		a.contextMu.Lock()
		obs = a.tokenObservations[key]
		if !obs.loaded {
			obs.Saved, obs.loaded = saved, true
			if a.tokenObservations == nil {
				a.tokenObservations = map[string]tokenObservation{}
			}
			a.tokenObservations[key] = obs
		}
		a.contextMu.Unlock()
	}
	status.ActualInputTokens = obs.Actual
	status.CalibrationSamples = obs.Samples
	// One measurement (now or saved) beats a guess that counts JSON at 2 bytes
	// a token; until three agree, the margin is wider.
	if ratio := max(obs.MaximumRatio, obs.Saved); ratio > 0 {
		marginPercent := 30
		if obs.Samples >= 3 {
			marginPercent = 20
		}
		margin := 1 + float64(marginPercent)/100
		factor := max(.35, ratio*margin)
		status.EstimatedInputTokens = int64(math.Ceil(float64(status.EstimatedInputTokens) * factor))
		status.Method += fmt.Sprintf("; calibrated against provider usage with %d%% margin, remains an estimate", marginPercent)
	}
	if status.ContextSource == "" {
		status.ContextSource = "model metadata (estimated when unavailable)"
	}
	status.Fits = status.EstimatedInputTokens+status.ReservedOutputTokens+status.SafetyTokens <= status.ContextWindow
	return status
}
