package agent

import (
	"context"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"sync"
	"time"
)

// PhaseMetrics contains cumulative observed wall time for one session in this
// process. Provider time includes transport waits, stream processing and message
// persistence; tool time includes approvals/sandbox waits inside Tool.Run.
// These observations do not measure pure inference or prove a speedup.
type PhaseMetrics struct {
	ContextMS         int64 `json:"context_ms"`
	ProviderMS        int64 `json:"provider_ms"`
	ToolsMS           int64 `json:"tools_ms"`
	FirstTokenMS      int64 `json:"first_token_ms"`
	FirstTokenSamples int64 `json:"first_token_samples"`
	ProviderCalls     int64 `json:"provider_calls"`
	ToolCalls         int64 `json:"tool_calls"`
	Repairs           int64 `json:"repairs"`
}

type UsageMetrics struct {
	PhaseMetrics

	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	Requests          int   `json:"requests"`
}
type UsageControl interface {
	Usage(context.Context, string) (UsageMetrics, error)
}

func (a *agent) Usage(ctx context.Context, id string) (UsageMetrics, error) {
	if err := ctx.Err(); err != nil {
		return UsageMetrics{}, err
	}
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	return a.usageMetrics[id], nil
}
func (a *agent) observeUsage(id string, u provider.TokenUsage) {
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	if a.usageMetrics == nil {
		a.usageMetrics = map[string]UsageMetrics{}
	}
	m := a.usageMetrics[id]
	m.InputTokens += u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens
	m.OutputTokens += u.OutputTokens
	m.CachedInputTokens += u.CacheReadTokens
	m.Requests++
	a.usageMetrics[id] = m
}

// startPhase returns an idempotent completion hook so early errors/cancellation
// and the normal path account for the same observation exactly once.
func (a *agent) startPhase(id, phase string) func() {
	start := time.Now()
	var once sync.Once
	return func() { once.Do(func() { a.observePhase(id, phase, time.Since(start)) }) }
}
func (a *agent) observePhase(id, phase string, elapsed time.Duration) {
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	if a.usageMetrics == nil {
		a.usageMetrics = map[string]UsageMetrics{}
	}
	m := a.usageMetrics[id]
	ms := max(int64(0), elapsed.Milliseconds())
	switch phase {
	case "context":
		m.ContextMS += ms
	case "provider":
		m.ProviderMS += ms
		m.ProviderCalls++
	case "tools":
		m.ToolsMS += ms
		m.ToolCalls++
	case "first_token":
		m.FirstTokenMS += ms
		m.FirstTokenSamples++
	case "repair":
		m.Repairs++
	default:
		return
	}
	a.usageMetrics[id] = m
}
func (a *agent) runTimedTool(id string, call func() (tools.ToolResponse, error)) (tools.ToolResponse, error) {
	stop := a.startPhase(id, "tools")
	defer stop()
	return call()
}
func generatedEvent(event provider.ProviderEvent) bool {
	switch event.Type {
	case provider.EventContentDelta:
		return event.Content != ""
	case provider.EventThinkingDelta:
		return event.Thinking != ""
	case provider.EventToolUseStart, provider.EventToolUseDelta:
		return event.ToolCall != nil
	case provider.EventComplete:
		return event.Response != nil && (event.Response.Content != "" || len(event.Response.ToolCalls) > 0)
	}
	return false
}
