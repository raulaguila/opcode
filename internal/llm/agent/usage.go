package agent

import (
	"context"
	"github.com/raulaguila/opcode/internal/llm/provider"
)

type UsageMetrics struct {
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
