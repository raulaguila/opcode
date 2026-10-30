package agent

import (
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/tiktoken-go/tokenizer"
)

// Stream sampling retains a transient, bounded text buffer only for a known
// compatible tokenizer. It is never included in Usage, logs or persistence.
// All access is protected by contextMu; one instance belongs to one attempt.
type streamUsage struct {
	model       models.ModelID
	firstOutput time.Time
	lastSample  time.Time
	outputBytes int64
	firstBytes  int64
	samples     int
	codec       tokenizer.Codec
	text        string
	firstTokens int
	tokens      float64
	source      string
}

const streamTokenizerLimit = 16 << 10

func (a *agent) startStreamUsage(id string, model models.ModelID, definitions ...models.Model) *streamUsage {
	s := &streamUsage{model: model, source: "bytes/4 estimate"}
	if len(definitions) > 0 && definitions[0].Provider == models.ProviderOpenAI {
		// An OpenAI-looking local name does not establish tokenizer compatibility.
		s.codec, _ = tokenizer.ForModel(tokenizer.Model(definitions[0].APIModel))
	}
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
	a.observeStreamUsageAt(s, event, time.Now())
}

func (a *agent) observeStreamUsageAt(s *streamUsage, event provider.ProviderEvent, now time.Time) {
	text := streamEventText(event)
	if text == "" {
		return
	}
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	s.outputBytes += int64(len(text))
	s.samples++
	if s.samples == 1 {
		s.firstOutput, s.firstBytes = now, s.outputBytes
	}
	if s.sampleTokens(text, now) {
		return
	}
	s.lastSample, s.source = now, "bytes/4 estimate"
	s.tokens = float64(s.outputBytes-s.firstBytes) / 4
}

func (a *agent) finishStreamUsage(id string, s *streamUsage) {
	a.contextMu.Lock()
	defer a.contextMu.Unlock()
	if a.streamUsages[id] == s {
		delete(a.streamUsages, id)
	}
	s.text, s.codec = "", nil
}

func streamEventText(event provider.ProviderEvent) string {
	var text string
	switch event.Type {
	case provider.EventContentDelta:
		text = event.Content
	case provider.EventThinkingDelta:
		// Some adapters populate both fields with the same reasoning delta.
		text = event.Thinking
		if text == "" {
			text = event.Content
		}
	case provider.EventToolUseDelta:
		if event.ToolCall != nil {
			text = event.ToolCall.Input
		}
	}

	return text
}

// Caller holds contextMu; tokenization remains bounded and rate limited.
func (s *streamUsage) sampleTokens(text string, now time.Time) bool {
	if s.codec == nil {
		return false
	}
	if len(s.text)+len(text) > streamTokenizerLimit {
		s.codec, s.text = nil, ""
		return false
	}
	s.text += text
	if s.samples != 1 && now.Sub(s.lastSample) < 250*time.Millisecond {
		return true
	}
	count, err := s.codec.Count(s.text)
	if err != nil {
		s.codec, s.text = nil, ""
		return false
	}
	if s.samples == 1 {
		s.firstTokens = count
	}
	s.tokens = float64(max(0, count-s.firstTokens))
	s.lastSample, s.source = now, "compatible "+s.codec.GetName()+" estimate"
	return true
}
