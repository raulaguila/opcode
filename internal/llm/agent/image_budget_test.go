package agent

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestVisionContextBudgetDoesNotTokenizeBase64OrShrinkVisualReserve(t *testing.T) {
	model := models.Model{Provider: models.ProviderOpenAI, APIModel: "gpt-4o", ContextWindow: 128000, DefaultMaxTokens: 2048, SupportsAttachments: true}
	p, err := provider.NewProvider(model.Provider, provider.WithModel(model), provider.WithAPIKey("fixture"), provider.WithMaxTokens(2048))
	require.NoError(t, err)
	a := &agent{provider: p}
	var first int64
	for _, compression := range []png.CompressionLevel{png.BestCompression, png.NoCompression} {
		var data bytes.Buffer
		encoder := png.Encoder{CompressionLevel: compression}
		require.NoError(t, encoder.Encode(&data, image.NewRGBA(image.Rect(0, 0, 512, 512))))
		history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Describe"}, message.BinaryContent{MIMEType: "image/png", Data: data.Bytes()}}}}
		status, err := a.estimateProviderContext(p, history, nil)
		require.NoError(t, err)
		require.True(t, status.Fits)
		if first > 0 {
			require.Equal(t, first, status.EstimatedInputTokens)
		}
		first = status.EstimatedInputTokens
		footprint, err := p.(provider.RequestSizer).RequestFootprint(history, nil)
		require.NoError(t, err)
		a.tokenObservations = map[string]tokenObservation{observationKey(model): {Samples: 3, MaximumRatio: .01}}
		calibrated, err := a.estimateProviderContext(p, history, nil)
		require.NoError(t, err)
		require.GreaterOrEqual(t, calibrated.EstimatedInputTokens, footprint.VisualTokens)
		a.tokenObservations = nil
	}
}
