package agent

import (
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
)

// streamUsage retains sizes and timing only, never generated text. All access
// is protected by contextMu. One instance belongs to one provider attempt.
type streamUsage struct {
	model       models.ModelID
	firstOutput time.Time
	outputBytes int64
}

func (a *agent) startStreamUsage(id string, model models.ModelID) *streamUsage {
	s := &streamUsage{model: model}
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	if a.streamUsages == nil {
		a.streamUsages = map[string]*streamUsage{}
	}
	a.streamUsages[id] = s
	if a.usageMetrics != nil {
		m := a.usageMetrics[id]
		m.LastModel, m.LastOutputTokens, m.LastOutputDurationNS, m.LastDurationSource = "", 0, 0, ""
		a.usageMetrics[id] = m
	}
	return s
}

func (a *agent) observeStreamUsage(s *streamUsage, event provider.ProviderEvent) {
	var size int
	switch event.Type {
	case provider.EventContentDelta:
		size = len(event.Content)
	case provider.EventThinkingDelta:
		// Some adapters populate both fields with the same reasoning delta.
		size = len(event.Thinking)
		if size == 0 {
			size = len(event.Content)
		}
	case provider.EventToolUseDelta:
		if event.ToolCall != nil {
			size = len(event.ToolCall.Input)
		}
	}
	if size == 0 {
		return
	}
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	if s.firstOutput.IsZero() {
		s.firstOutput = time.Now()
	}
	s.outputBytes += int64(size)
}

func (a *agent) finishStreamUsage(id string, s *streamUsage) {
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	if a.streamUsages[id] == s {
		delete(a.streamUsages, id)
	}
}
