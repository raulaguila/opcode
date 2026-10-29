package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestStreamUsageCountsOutputNotPacketsOrDuplicatedThinking(t *testing.T) {
	a := &agent{}
	whole := a.startStreamUsage("whole", "m")
	split := a.startStreamUsage("split", "m")
	text := "ação 漢字 code"
	a.observeStreamUsage(whole, provider.ProviderEvent{Type: provider.EventContentDelta, Content: text})
	for _, fragment := range strings.Split(text, "") {
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
