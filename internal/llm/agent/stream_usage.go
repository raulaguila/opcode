package agent

import (
	"strings"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/tiktoken-go/tokenizer"
)

// Stream sampling retains a transient, bounded text buffer only for a known
// compatible tokenizer. It is never included in Usage, logs or persistence.
// Published counters use contextMu. Tokenization has its own per-attempt lock
// and never holds the agent-wide mutex while scanning text.
type streamUsage struct {
	model       models.ModelID
	firstOutput time.Time
	lastSample  time.Time
	outputBytes int64
	firstBytes  int64
	samples     int
	samplingMu  sync.Mutex
	sampler     streamTokenSampler
	tokens      float64
	source      string
}

const streamTokenizerLimit = 16 << 10

func (a *agent) startStreamUsage(id string, model models.ModelID, definitions ...models.Model) *streamUsage {
	s := &streamUsage{model: model, source: "bytes/4 estimate"}
	if len(definitions) > 0 && definitions[0].Provider == models.ProviderOpenAI {
		// An OpenAI-looking local name does not establish tokenizer compatibility.
		s.sampler.codec, _ = tokenizer.ForModel(tokenizer.Model(definitions[0].APIModel))
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
	s.samplingMu.Lock()
	defer s.samplingMu.Unlock()
	a.contextMu.Lock()
	s.outputBytes += int64(len(text))
	s.samples++
	if s.samples == 1 {
		s.firstOutput, s.firstBytes = now, s.outputBytes
	}
	first := s.samples == 1
	bytes := s.outputBytes - s.firstBytes
	a.contextMu.Unlock()
	if !s.sampler.sampleTokens(text, now, first) {
		s.sampler.lastSample, s.sampler.source = now, "bytes/4 estimate"
		s.sampler.tokens = float64(bytes) / 4
	}
	a.contextMu.Lock()
	s.lastSample, s.source, s.tokens = s.sampler.lastSample, s.sampler.source, s.sampler.tokens
	a.contextMu.Unlock()
}

func (a *agent) finishStreamUsage(id string, s *streamUsage) {
	s.samplingMu.Lock()
	defer s.samplingMu.Unlock()
	a.contextMu.Lock()
	if a.streamUsages[id] == s {
		delete(a.streamUsages, id)
	}
	a.contextMu.Unlock()
	s.sampler.text.Reset()
	s.sampler.codec = nil
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

type streamTokenSampler struct {
	codec       tokenizer.Codec
	text        strings.Builder
	firstTokens int
	lastSample  time.Time
	tokens      float64
	source      string
}

// Caller holds only the per-stream sampling mutex. Work remains bounded and
// rate limited, and cannot block metrics or context updates of another stream.
func (s *streamTokenSampler) sampleTokens(text string, now time.Time, first bool) bool {
	if s.codec == nil {
		return false
	}
	if s.text.Len()+len(text) > streamTokenizerLimit {
		s.codec = nil
		s.text.Reset()
		return false
	}
	s.text.WriteString(text)
	if !first && now.Sub(s.lastSample) < 250*time.Millisecond {
		return true
	}
	count, err := s.codec.Count(s.text.String())
	if err != nil {
		s.codec = nil
		s.text.Reset()
		return false
	}
	if first {
		s.firstTokens = count
	}
	s.tokens = float64(max(0, count-s.firstTokens))
	s.lastSample, s.source = now, "compatible "+s.codec.GetName()+" estimate"
	return true
}
