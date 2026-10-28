package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type contextFailProvider struct {
	*controlledProvider
	window  int64
	calls   *atomic.Int32
	failure error
	partial bool
}

func (p *contextFailProvider) Model() models.Model {
	return models.Model{ID: "adaptive-test", Provider: models.ProviderOpenAI, APIModel: "adaptive", ContextWindow: p.window, DefaultMaxTokens: 1024}
}
func (p *contextFailProvider) WithContextWindow(window int64) (provider.Provider, error) {
	return &contextFailProvider{controlledProvider: p.controlledProvider, window: window, calls: p.calls, failure: p.failure, partial: p.partial}, nil
}
func (p *contextFailProvider) StreamResponse(ctx context.Context, h []message.Message, ts []tools.BaseTool) <-chan provider.ProviderEvent {
	if p.calls.Add(1) == 1 {
		out := make(chan provider.ProviderEvent, 2)
		if p.partial {
			call := toolCall("edit", `{}`)
			out <- provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCall: &call}
		}
		out <- provider.ProviderEvent{Type: provider.EventError, Error: p.failure}
		close(out)
		return out
	}
	return p.controlledProvider.StreamResponse(ctx, h, ts)
}
func TestContextFailureRetriesOnlyGenerationAndPersistsPrivateLimit(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	config.Get().Data.Directory = t.TempDir()
	broken := &contextFailProvider{controlledProvider: p, window: 32768, calls: &atomic.Int32{}, failure: &provider.HTTPError{StatusCode: 400, Body: "maximum context length exceeded"}}
	a.provider = broken
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	next := nextGeneration(t, p)
	next.reply <- provider.ProviderResponse{Content: "Inspecionado.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	status, statusErr := a.ContextStatus(context.Background(), "s")
	require.NoError(t, statusErr)
	require.EqualValues(t, 16384, status.ContextWindow)
	require.EqualValues(t, 16384, learnedContext(broken.Model()))
	path, _ := calibrationLocation(broken.Model())
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "adaptive")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.EqualValues(t, 0600, info.Mode().Perm())
}
func TestContextFailureAfterPartialToolCallIsNeverRetried(t *testing.T) {
	a, p, m, _ := runtimeAgent(t, workflow.Execute)
	broken := &contextFailProvider{controlledProvider: p, window: 32768, calls: &atomic.Int32{}, failure: fmt.Errorf("maximum context length exceeded"), partial: true}
	a.provider = broken
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	require.Error(t, finishGeneration(t, done).Error)
	require.EqualValues(t, 1, broken.calls.Load())
	history, err := m.List(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, message.Tool, history[len(history)-1].Role)
	require.True(t, history[len(history)-1].ToolResults()[0].IsError)
}
func TestContextCalibrationExpiresAndDoesNotCrossConnections(t *testing.T) {
	_, _, _, _ = runtimeAgent(t, workflow.Execute)
	config.Get().Data.Directory = t.TempDir()
	model := models.Model{ID: "x", Provider: models.ProviderOpenAI, APIModel: "x"}
	require.NoError(t, saveContextCalibration(model, 8192))
	require.EqualValues(t, 8192, learnedContext(model))
	model.APIModel = "other"
	require.Zero(t, learnedContext(model))
	model.APIModel = "x"
	path, key := calibrationLocation(model)
	require.NoError(t, localstate.Write(path, map[string]contextCalibration{key: {Window: 8192, Expires: time.Now().Add(-time.Second).Unix()}}, calibrationLimit))
	require.Zero(t, learnedContext(model))
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("invalid API key"), &provider.HTTPError{StatusCode: 401, Body: "context window"}} {
		require.False(t, contextFailure(err, model))
	}
	local := model
	local.Provider = models.ProviderLocal
	require.True(t, contextFailure(errors.New("Ollama out of memory"), local))
	require.False(t, contextFailure(errors.New(strings.Repeat("failed", 5)), local))
}
