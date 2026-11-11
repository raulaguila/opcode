package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type finalProtocolProbe struct {
	provider.Provider
	calls              atomic.Int32
	failFrom, failures int32
	partial            bool
	failure            error
}

func (p *finalProtocolProbe) StreamResponse(ctx context.Context, h []message.Message, ts []tools.BaseTool) <-chan provider.ProviderEvent {
	n := p.calls.Add(1)
	if n >= p.failFrom && n < p.failFrom+p.failures {
		events := make(chan provider.ProviderEvent, 2)
		if p.partial {
			events <- provider.ProviderEvent{Type: provider.EventContentDelta, Content: "partial"}
		}
		failure := p.failure
		if failure == nil {
			failure = provider.ErrInvalidFinalAnswer
		}
		events <- provider.ProviderEvent{Type: provider.EventError, Error: failure}
		close(events)
		return events
	}
	return p.Provider.StreamResponse(ctx, h, ts)
}

func TestFinalProtocolRepairDoesNotReplayCompletedOperation(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	probe := &finalProtocolProbe{Provider: p, failFrom: 2, failures: 1}
	a.provider = probe
	command := &countedTool{name: "bash", output: "command completed"}
	a.tools = []tools.BaseTool{command}
	done, err := a.Run(context.Background(), "s", "Execute bash e informe o resultado.")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("bash", `{}`))
	repaired := nextGeneration(t, p)
	require.Contains(t, lastSent(repaired.history).Content().Text, "do not repeat edits or commands")
	repaired.reply <- provider.ProviderResponse{Content: "Comando concluído.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	require.EqualValues(t, 1, command.count.Load())
	require.EqualValues(t, 3, probe.calls.Load())
}

func TestFinalProtocolRepairIsBoundedAndRefusesPartialOutput(t *testing.T) {
	for _, partial := range []bool{false, true} {
		a, p, _, _ := runtimeAgent(t, workflow.Ask)
		probe := &finalProtocolProbe{Provider: p, failFrom: 1, failures: 2, partial: partial}
		a.provider = probe
		done, err := a.Run(context.Background(), "s", "Responda ok.")
		require.NoError(t, err)
		require.ErrorIs(t, finishGeneration(t, done).Error, provider.ErrInvalidFinalAnswer)
		expected := int32(2)
		if partial {
			expected = 1
		}
		require.Equal(t, expected, probe.calls.Load())
	}
}

func TestOutputLimitDoesNotRetryOrReplayCompletedOperation(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	probe := &finalProtocolProbe{Provider: p, failFrom: 2, failures: 1, failure: provider.ErrOutputLimit}
	a.provider = probe
	command := &countedTool{name: "bash", output: "command completed"}
	a.tools = []tools.BaseTool{command}
	done, err := a.Run(context.Background(), "s", "Execute bash e informe o resultado.")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("bash", `{}`))
	result := finishGeneration(t, done)
	require.ErrorIs(t, result.Error, provider.ErrOutputLimit)
	require.ErrorContains(t, result.Error, "increase output tokens")
	require.False(t, contextFailure(result.Error, p.Model()))
	require.EqualValues(t, 1, command.count.Load())
	require.EqualValues(t, 2, probe.calls.Load(), "no automatic retry or citation repair")
}
