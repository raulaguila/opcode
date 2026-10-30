package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func budgetPNG(t testing.TB, size int, compression png.CompressionLevel) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: compression}
	require.NoError(t, encoder.Encode(&out, image.NewRGBA(image.Rect(0, 0, size, size))))
	return out.Bytes()
}

func TestImageFootprintDependsOnDimensionsNotPNGCompression(t *testing.T) {
	for _, variant := range []struct {
		backend models.ModelProvider
		api     string
	}{
		{models.ProviderOpenAI, "chat_completions"}, {models.ProviderOpenAI, "responses"}, {models.ProviderAnthropic, ""}, {models.ProviderGemini, ""}, {models.ProviderLocal, "ollama"},
	} {
		t.Run(string(variant.backend)+"/"+variant.api, func(t *testing.T) {
			model := models.Model{Provider: variant.backend, ProviderProfile: "fixture", APIModel: "gpt-4o", API: variant.api, ContextWindow: 128000}
			p, err := NewProvider(model.Provider, WithModel(model), WithAPIKey("fixture-key"), WithBaseURL("http://127.0.0.1:11434/v1"))
			require.NoError(t, err)
			var prior RequestFootprint
			for _, level := range []png.CompressionLevel{png.BestCompression, png.NoCompression} {
				data := budgetPNG(t, 512, level)
				history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Describe"}, message.BinaryContent{MIMEType: "image/png", Data: data}}}}
				prepared, err := p.(RequestPreparer).PrepareRequest(history, nil, true)
				require.NoError(t, err)
				footprint, err := prepared.RequestFootprint()
				require.NoError(t, err)
				require.Positive(t, footprint.VisualTokens)
				require.True(t, bytes.Contains(prepared.(*preparedHTTP).body, []byte(base64.StdEncoding.EncodeToString(data))), "the actual request keeps the complete image")
				if prior.WireBytes > 0 {
					require.Equal(t, prior.VisualTokens, footprint.VisualTokens)
					require.Equal(t, prior.ASCIIBytes, footprint.ASCIIBytes)
					require.Equal(t, prior.TokenizedPayloadTokens, footprint.TokenizedPayloadTokens)
					require.Greater(t, footprint.WireBytes, prior.WireBytes)
				}
				prior = footprint
			}
		})
	}
}

func TestImageProjectionKeepsTextSchemasAndToolArgumentsIntact(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,IMAGE"}},{"type":"text","text":"data:image/png;base64,TEXT"}]},{"role":"assistant","tool_calls":[{"arguments":{"type":"image","source":{"type":"base64","data":"ARGUMENT"}}}]}],"tools":[{"parameters":{"type":"image","source":{"type":"base64","data":"SCHEMA"}}}]}`)
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.BinaryContent{MIMEType: "image/png", Data: budgetPNG(t, 1, png.BestCompression)}}}}
	projected, _, err := imageBudgetProjection(models.Model{}, history, raw)
	require.NoError(t, err)
	require.NotContains(t, string(projected), "base64,IMAGE")
	for _, value := range []string{"base64,TEXT", "ARGUMENT", "SCHEMA"} {
		require.Contains(t, string(projected), value)
	}
}

func TestImageReservesRemainPositiveAndModelSpecific(t *testing.T) {
	for _, model := range []models.Model{
		{Provider: models.ProviderOpenAI, APIModel: "gpt-4o"},
		{Provider: models.ProviderOpenAI, APIModel: "gpt-4o-mini"},
		{Provider: models.ProviderOpenAI, APIModel: "gpt-4.1-mini"},
		{Provider: models.ProviderAnthropic, APIModel: "claude"},
		{Provider: models.ProviderLocal, APIModel: "gpt-4o"},
	} {
		reserve := imageTokenReserve(model, 512, 512)
		require.Positive(t, reserve)
		require.GreaterOrEqual(t, imageTokenReserve(model, 1024, 1024), reserve)
	}
	standard := models.Model{Provider: models.ProviderOpenAI, APIModel: "gpt-4o"}
	mini := standard
	mini.APIModel = "gpt-4o-mini"
	require.Greater(t, imageTokenReserve(mini, 512, 512), imageTokenReserve(standard, 512, 512))
	require.GreaterOrEqual(t, imageTokenReserve(standard, 512, 512), int64(85+170*4), "reserve the older high-detail upscale interpretation as well")
}

func TestImageURLContentCannotBypassTheVisualReserve(t *testing.T) {
	p, err := NewProvider(models.ProviderOpenAI, WithAPIKey("fixture"), WithModel(models.Model{Provider: models.ProviderOpenAI, APIModel: "gpt-4o"}))
	require.NoError(t, err)
	for _, url := range []string{"https://private.invalid/image.png", "data:image/png;base64,ignored", ""} {
		history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.ImageURLContent{URL: url}}}}
		_, err := p.(RequestPreparer).PrepareRequest(history, nil, true)
		require.ErrorContains(t, err, "attach image bytes")
		_, err = p.SendMessages(context.Background(), history, nil)
		require.ErrorContains(t, err, "attach image bytes")
		event := <-p.StreamResponse(context.Background(), history, nil)
		require.Equal(t, EventError, event.Type)
		require.ErrorContains(t, event.Error, "attach image bytes")
	}
}

func TestImageDataURLIsNormalizedAndBudgetedWithoutChangingHistory(t *testing.T) {
	data := budgetPNG(t, 512, png.BestCompression)
	source := message.ImageURLContent{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)}
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{source}}}
	p, err := NewProvider(models.ProviderOpenAI, WithAPIKey("fixture"), WithModel(models.Model{Provider: models.ProviderOpenAI, APIModel: "gpt-4o"}))
	require.NoError(t, err)
	prepared, err := p.(RequestPreparer).PrepareRequest(history, nil, true)
	require.NoError(t, err)
	footprint, err := prepared.RequestFootprint()
	require.NoError(t, err)
	require.Positive(t, footprint.VisualTokens)
	require.Equal(t, source, history[0].Parts[0])
	require.True(t, bytes.Contains(prepared.(*preparedHTTP).body, []byte(source.URL)))
}

func TestImageBudgetRejectsInvalidDimensionsAndLargeWireBody(t *testing.T) {
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.BinaryContent{MIMEType: "image/png", Data: []byte("invalid")}}}}
	_, _, err := imageBudgetProjection(models.Model{}, history, []byte(`{}`))
	require.Error(t, err)
	p, err := NewProvider(models.ProviderOpenAI, WithAPIKey("fixture"), WithModel(models.Model{Provider: models.ProviderOpenAI, APIModel: "fixture"}))
	require.NoError(t, err)
	history = []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("x", maxProviderBytes)}}}}
	_, err = p.(RequestPreparer).PrepareRequest(history, nil, true)
	require.ErrorContains(t, err, "request exceeds 32 MiB")
}

func TestHTTPErrorJSONDoesNotPublishPrivateFields(t *testing.T) {
	raw, err := json.Marshal(&HTTPError{StatusCode: 400, RequestID: "private-id", Body: "private-secret"})
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private")
}
