package agent

import (
	"fmt"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func allocationContextProvider(t testing.TB) provider.Provider {
	t.Helper()
	model := models.Model{Provider: models.ProviderLocal, APIModel: "fixture", ContextWindow: 1000000, DefaultMaxTokens: 2048}
	p, err := provider.NewProvider(model.Provider, provider.WithModel(model), provider.WithBaseURL("http://127.0.0.1:11434/v1"), provider.WithMaxTokens(2048))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestContextSizingRetainsSerializedHardBudget(t *testing.T) {
	p := allocationContextProvider(t)
	a := &agent{provider: p, systemMessage: "planning instructions"}
	for _, size := range []int{100, 150000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			history := []message.Message{
				{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("ação \"text\"\n", size)}}},
				{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{Content: "returned source", Metadata: `{"source":"source.go"}`}}},
			}
			footprint, err := p.(provider.RequestSizer).RequestFootprint(history, nil)
			require.NoError(t, err)
			status, err := a.estimateProviderContext(p, history, nil)
			require.NoError(t, err)
			expected := (footprint.ASCIIBytes+1)/2 + footprint.OtherBytes + int64(len(history))*32 + 256 + footprint.VisualTokens
			require.Equal(t, expected, status.EstimatedInputTokens)
			require.Equal(t, footprint.ReservedOutputTokens, status.ReservedOutputTokens)
			require.Equal(t, expected+status.ReservedOutputTokens+status.SafetyTokens <= status.ContextWindow, status.Fits)
			require.Positive(t, status.Components["evidence"])
		})
	}
}

func BenchmarkContextPreparation(b *testing.B) {
	p := allocationContextProvider(b)
	a := &agent{provider: p}
	history := make([]message.Message, 20)
	for i := range history {
		history[i] = message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("text ", 1600)}}}
	}
	b.Run("prepare_only", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := p.(provider.RequestPreparer).PrepareRequest(history, nil, true); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("agent_estimate", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := a.estimateProviderContext(p, history, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}
