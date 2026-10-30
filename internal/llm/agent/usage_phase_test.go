package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestPhaseMetricsAccumulateSafelyAndStopExactlyOnce(t *testing.T) {
	a := &agent{}
	var workers sync.WaitGroup
	for range 40 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			a.observePhase("session", "provider", 5*time.Millisecond)
			a.observePhase("session", "first_token", time.Millisecond)
			a.observePhase("session", "repair", 0)
		}()
	}
	workers.Wait()
	got, err := a.Usage(context.Background(), "session")
	require.NoError(t, err)
	require.EqualValues(t, 200, got.ProviderMS)
	require.EqualValues(t, 40, got.ProviderCalls)
	require.EqualValues(t, 40, got.FirstTokenSamples)
	require.EqualValues(t, 40, got.FirstTokenMS)
	require.EqualValues(t, 40, got.Repairs)
	stop := a.startPhase("once", "provider")
	stop()
	stop()
	once, err := a.Usage(context.Background(), "once")
	require.NoError(t, err)
	require.EqualValues(t, 1, once.ProviderCalls)
	empty, err := a.Usage(context.Background(), "other")
	require.NoError(t, err)
	require.Zero(t, empty)
}
func TestTimedToolIncludesFailureWithoutReexecuting(t *testing.T) {
	a := &agent{}
	calls := 0
	_, err := a.runTimedTool("session", func() (tools.ToolResponse, error) {
		calls++
		return tools.ToolResponse{}, permission.ErrorPermissionDenied
	})
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Equal(t, 1, calls)
	got, e := a.Usage(context.Background(), "session")
	require.NoError(t, e)
	require.EqualValues(t, 1, got.ToolCalls)
}
func TestProviderPhaseIncludesCancellationWithoutInventingFirstToken(t *testing.T) {
	a, p, _ := steeringAgent(t)
	events, err := a.Run(context.Background(), "session", "Responda somente OK.")
	require.NoError(t, err)
	_ = nextGeneration(t, p)
	a.Cancel("session")
	result := finishGeneration(t, events)
	require.True(t, errors.Is(result.Error, ErrRequestCancelled) || errors.Is(result.Error, context.Canceled))
	got, e := a.Usage(context.Background(), "session")
	require.NoError(t, e)
	require.EqualValues(t, 1, got.ProviderCalls)
	require.Zero(t, got.FirstTokenSamples)
	require.Zero(t, got.ToolCalls)
}
func TestProviderAndToolPhasesObserveRealRuntimeCalls(t *testing.T) {
	a, p, _ := steeringAgent(t)
	read := &countedTool{name: "view", output: "real bounded result"}
	a.tools = []tools.BaseTool{read}
	events, err := a.Run(context.Background(), "session", "Responda somente OK.")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{}`))
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "OK", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, events).Error)
	got, e := a.Usage(context.Background(), "session")
	require.NoError(t, e)
	require.EqualValues(t, 2, got.ProviderCalls)
	require.EqualValues(t, 1, got.ToolCalls)
	require.EqualValues(t, 2, got.FirstTokenSamples)
	require.Zero(t, got.Repairs)
}
func TestOnlyGeneratedProviderEventsCountAsFirstToken(t *testing.T) {
	for _, event := range []provider.ProviderEvent{{Type: provider.EventContentStart}, {Type: provider.EventWarning, Content: "warning"}, {Type: provider.EventError, Error: errors.New("failure")}, {Type: provider.EventComplete, Response: &provider.ProviderResponse{}}} {
		require.False(t, generatedEvent(event))
	}
	require.True(t, generatedEvent(provider.ProviderEvent{Type: provider.EventThinkingDelta, Thinking: "thinking"}))
	require.True(t, generatedEvent(provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCall: &message.ToolCall{Name: "view"}}))
}
