package agent

import (
	"slices"
	"time"

	"github.com/raulaguila/opcode/internal/llm/tools"
)

// PerformanceMetrics is an observed session diagnostic, not an inference-speed
// claim. Percentiles use the last 64 provider first-output samples, including
// client/transport waits. Counters contain no source text or request arguments.
type PerformanceMetrics struct {
	PreparationCalls int64 `json:"preparation_calls"`
	// Non-reused view invocations, including failures. Revalidation can still
	// read/hash the source; this counter is not a count of filesystem I/O.
	PreparationSourceReads int64 `json:"preparation_source_reads"`
	PreparationReuses      int64 `json:"preparation_reuses"`
	PreparationBytes       int64 `json:"preparation_bytes"`
	LocalTitles            int64 `json:"local_titles_without_inference"`
	FirstTokenSamples      int   `json:"first_token_window_samples"`
	FirstTokenLastMS       int64 `json:"first_token_last_ms"`
	FirstTokenP50MS        int64 `json:"first_token_p50_ms"`
	FirstTokenP95MS        int64 `json:"first_token_p95_ms"`
}

type performanceState struct {
	metrics     PerformanceMetrics
	firstTokens []int64
}

// Caller holds contextMu, matching all other session usage state.
func (a *agent) performanceLocked(id string) *performanceState {
	if a.performance == nil {
		a.performance = map[string]*performanceState{}
	}
	if a.performance[id] == nil {
		a.performance[id] = &performanceState{}
	}
	return a.performance[id]
}

func (a *agent) observePreparation(id, tool string, reused bool, bytes int) {
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	s := a.performanceLocked(id)
	s.metrics.PreparationCalls++
	s.metrics.PreparationBytes += int64(bytes)
	if reused {
		s.metrics.PreparationReuses++
	} else if tool == tools.ViewToolName {
		s.metrics.PreparationSourceReads++
	}
}

func (a *agent) observeLocalTitle(id string) {
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	a.performanceLocked(id).metrics.LocalTitles++
}

func (a *agent) observeFirstTokenLocked(id string, elapsed time.Duration) {
	s := a.performanceLocked(id)
	s.firstTokens = append(s.firstTokens, max(int64(0), elapsed.Milliseconds()))
	if len(s.firstTokens) > 64 {
		s.firstTokens = slices.Clone(s.firstTokens[len(s.firstTokens)-64:])
	}
}

func (s *performanceState) snapshot() PerformanceMetrics {
	m := s.metrics
	n := len(s.firstTokens)
	if n == 0 {
		return m
	}
	m.FirstTokenSamples = n
	m.FirstTokenLastMS = s.firstTokens[n-1]
	ordered := slices.Clone(s.firstTokens)
	slices.Sort(ordered)
	m.FirstTokenP50MS = ordered[(50*n+99)/100-1]
	m.FirstTokenP95MS = ordered[(95*n+99)/100-1]
	return m
}
