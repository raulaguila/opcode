package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
	"github.com/tiktoken-go/tokenizer"
)

func TestStreamUsageCountsOutputNotPacketsOrDuplicatedThinking(t *testing.T) {
	a := &agent{}
	whole := a.startStreamUsage("whole", "m")
	split := a.startStreamUsage("split", "m")
	text := "ação 漢字 code"
	a.observeStreamUsage(whole, provider.ProviderEvent{Type: provider.EventContentDelta, Content: text})
	for fragment := range strings.SplitSeq(text, "") {
		a.observeStreamUsage(split, provider.ProviderEvent{Type: provider.EventContentDelta, Content: fragment})
	}
	require.Equal(t, whole.outputBytes, split.outputBytes)
	require.EqualValues(t, len(text), whole.outputBytes)
	for _, event := range []provider.ProviderEvent{
		{Type: provider.EventContentStart},
		{Type: provider.EventWarning, Content: "not generated"},
		{Type: provider.EventToolUseStart, ToolCall: &message.ToolCall{ID: "metadata", Name: "view"}},
		{Type: provider.EventComplete, Response: &provider.ProviderResponse{Content: text}},
	} {
		a.observeStreamUsage(whole, event)
	}
	require.EqualValues(t, len(text), whole.outputBytes, "completion must not count already streamed text again")
	a.observeStreamUsage(whole, provider.ProviderEvent{Type: provider.EventThinkingDelta, Thinking: "thought", Content: "thought"})
	a.observeStreamUsage(whole, provider.ProviderEvent{Type: provider.EventToolUseDelta, ToolCall: &message.ToolCall{Input: `{"a":1}`}})
	require.EqualValues(t, len(text)+len("thought")+len(`{"a":1}`), whole.outputBytes)
	m, err := a.Usage(context.Background(), "whole")
	require.NoError(t, err)
	require.True(t, m.StreamActive)
	require.Positive(t, m.StreamDurationNS)
	other, err := a.Usage(context.Background(), "other")
	require.NoError(t, err)
	require.False(t, other.StreamActive)
	require.Zero(t, other.StreamOutputBytes)
	next := a.startStreamUsage("whole", "new-model")
	a.finishStreamUsage("whole", whole)
	m, err = a.Usage(context.Background(), "whole")
	require.NoError(t, err)
	require.EqualValues(t, "new-model", m.StreamModel)
	require.Zero(t, m.StreamOutputBytes)
	require.Zero(t, m.StreamDurationNS, "no elapsed generation time before the first output")
	a.finishStreamUsage("whole", next)
	m, err = a.Usage(context.Background(), "whole")
	require.NoError(t, err)
	require.False(t, m.StreamActive)
}

func TestStreamSamplesExcludeFirstBurstAndUseEventTimes(t *testing.T) {
	a := &agent{}
	s := a.startStreamUsage("s", "local")
	start := time.Now()
	a.observeStreamUsageAt(s, provider.ProviderEvent{Type: provider.EventContentDelta, Content: strings.Repeat("x", 800)}, start)
	u, err := a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, 1, u.StreamSamples)
	require.Zero(t, u.StreamSampleTokens)
	require.Zero(t, u.StreamSampleDurationNS)
	a.observeStreamUsageAt(s, provider.ProviderEvent{Type: provider.EventContentDelta, Content: "12345678"}, start.Add(time.Second))
	u, err = a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, 2.0, u.StreamSampleTokens)
	require.EqualValues(t, time.Second, u.StreamSampleDurationNS)
	require.Equal(t, "bytes/4 estimate", u.StreamCountSource)
	// Polling the UI cannot age a sample or manufacture additional tokens.
	v, err := a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, u.StreamSampleDurationNS, v.StreamSampleDurationNS)
	require.Equal(t, u.StreamSampleTokens, v.StreamSampleTokens)
}

func TestStreamCompatibleTokenizerIsBoundedAndNotInferredForLocalModels(t *testing.T) {
	a := &agent{}
	start := time.Now()
	local := a.startStreamUsage("local", "local.gpt", models.Model{Provider: models.ProviderLocal, APIModel: "gpt-4o"})
	require.Nil(t, local.sampler.codec)
	s := a.startStreamUsage("s", "openai.gpt", models.Model{Provider: models.ProviderOpenAI, APIModel: "gpt-4o"})
	require.NotNil(t, s.sampler.codec)
	first, second := "Olá mundo. ", "Ação e código são exemplos. "
	a.observeStreamUsageAt(s, provider.ProviderEvent{Type: provider.EventContentDelta, Content: first}, start)
	a.observeStreamUsageAt(s, provider.ProviderEvent{Type: provider.EventContentDelta, Content: second}, start.Add(time.Second))
	counter, err := tokenizer.ForModel("gpt-4o")
	require.NoError(t, err)
	initial, err := counter.Count(first)
	require.NoError(t, err)
	total, err := counter.Count(first + second)
	require.NoError(t, err)
	u, err := a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, float64(total-initial), u.StreamSampleTokens)
	require.Contains(t, u.StreamCountSource, "compatible")
	a.observeStreamUsageAt(s, provider.ProviderEvent{Type: provider.EventContentDelta, Content: strings.Repeat("x", streamTokenizerLimit)}, start.Add(2*time.Second))
	require.Nil(t, s.sampler.codec)
	require.Zero(t, s.sampler.text.Len())
	u, err = a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, "bytes/4 estimate", u.StreamCountSource)
	a.finishStreamUsage("s", s)
	require.Zero(t, s.sampler.text.Len())
}

type usageStreamProvider struct {
	provider.Provider
	events chan provider.ProviderEvent
}

func (p *usageStreamProvider) StreamResponse(context.Context, []message.Message, []tools.BaseTool) <-chan provider.ProviderEvent {
	return p.events
}

func TestStreamUsageAvailableBeforeCompletionAndClearedOnEveryExit(t *testing.T) {
	for _, ending := range []string{"complete", "cancel", "error", "disconnect"} {
		t.Run(ending, func(t *testing.T) {
			a, base, _ := steeringAgent(t)
			p := &usageStreamProvider{Provider: base, events: make(chan provider.ProviderEvent, 4)}
			a.provider = p
			a.observeUsage("session", provider.TokenUsage{OutputTokens: 99, OutputDurationNS: int64(time.Second)}, "test-model")
			done, err := a.Run(context.Background(), "session", "Responda somente OK.")
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				m, e := a.Usage(context.Background(), "session")
				return e == nil && m.StreamActive && m.StreamOutputBytes == 0 && m.LastOutputTokens == 0
			}, 3*time.Second, time.Millisecond)
			p.events <- provider.ProviderEvent{Type: provider.EventContentDelta, Content: "Streaming before completion"}
			require.Eventually(t, func() bool {
				m, e := a.Usage(context.Background(), "session")
				return e == nil && m.StreamActive && m.StreamOutputBytes > 0 && m.StreamDurationNS > 0
			}, 3*time.Second, time.Millisecond)
			switch ending {
			case "complete":
				p.events <- provider.ProviderEvent{Type: provider.EventComplete, Response: &provider.ProviderResponse{Content: "Streaming before completion", FinishReason: message.FinishReasonEndTurn, Usage: provider.TokenUsage{OutputTokens: 40, OutputDurationNS: int64(2 * time.Second), DurationSource: "provider"}}}
			case "cancel":
				a.Cancel("session")
			case "error":
				p.events <- provider.ProviderEvent{Type: provider.EventError, Error: errors.New("fixture failure")}
			case "disconnect":
				close(p.events)
			}
			result := finishGeneration(t, done)
			assertSavedStreamText(t, a, "Streaming before completion")
			if ending == "complete" {
				require.NoError(t, result.Error)
			} else {
				require.Error(t, result.Error)
			}
			m, err := a.Usage(context.Background(), "session")
			require.NoError(t, err)
			require.False(t, m.StreamActive)
			require.Zero(t, m.StreamOutputBytes)
			require.Zero(t, m.StreamDurationNS)
			if ending == "complete" {
				require.EqualValues(t, 40, m.LastOutputTokens)
				require.EqualValues(t, int64(2*time.Second), m.LastOutputDurationNS)
				require.Equal(t, "provider", m.LastDurationSource)
			} else {
				require.Zero(t, m.LastOutputTokens, "a failed stream must not revive speed from a previous answer")
			}
		})
	}
}

func assertSavedStreamText(t *testing.T, a *agent, expected string) {
	t.Helper()
	items, err := a.messages.List(context.Background(), "session")
	require.NoError(t, err)
	for _, item := range items {
		if item.Role == message.Assistant {
			require.Equal(t, expected, item.Content().Text)
			require.NotNil(t, item.FinishPart(), "all exits must persist a terminal checkpoint")
			return
		}
	}
	t.Fatal("streamed assistant message was not persisted")
}

type blockingStreamCodec struct {
	tokenizer.Codec
	started, release chan struct{}
}

func (*blockingStreamCodec) GetName() string { return "blocking-fixture" }
func (c *blockingStreamCodec) Count(text string) (int, error) {
	close(c.started)
	<-c.release
	return len(text), nil
}

func TestStreamTokenizationDoesNotHoldContextMutex(t *testing.T) {
	a := &agent{}
	s := a.startStreamUsage("s", "local")
	codec := &blockingStreamCodec{started: make(chan struct{}), release: make(chan struct{})}
	s.sampler.codec = codec
	done := make(chan struct{})
	go func() {
		a.observeStreamUsage(s, provider.ProviderEvent{Type: provider.EventContentDelta, Content: "first"})
		close(done)
	}()
	<-codec.started
	read := make(chan UsageMetrics, 1)
	go func() { usage, _ := a.Usage(context.Background(), "s"); read <- usage }()
	select {
	case usage := <-read:
		require.EqualValues(t, len("first"), usage.StreamOutputBytes)
	case <-time.After(time.Second):
		close(codec.release)
		<-done
		t.Fatal("usage blocked behind tokenization")
	}
	close(codec.release)
	<-done
	a.finishStreamUsage("s", s)
}
