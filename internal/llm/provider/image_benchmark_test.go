package provider

import (
	"bytes"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"image"
	"image/png"
	"testing"
)

// A real uncompressed PNG (~1MiB) exercises wire encoding and visual budgeting;
// no endpoint is contacted. Keep the fixture identical on both revisions.
func BenchmarkPreparedImageBudget(b *testing.B) {
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 512, 512))); err != nil {
		b.Fatal(err)
	}
	model := models.Model{Provider: models.ProviderOpenAI, APIModel: "fixture", ContextWindow: 1000000, DefaultMaxTokens: 2048, SupportsAttachments: true}
	p, err := NewProvider(model.Provider, WithModel(model), WithAPIKey("fixture"), WithMaxTokens(2048))
	if err != nil {
		b.Fatal(err)
	}
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Describe the attached fixture"}, message.BinaryContent{MIMEType: "image/png", Data: encoded.Bytes()}}}}
	b.ReportAllocs()
	b.SetBytes(int64(encoded.Len()))
	b.ResetTimer()
	for range b.N {
		prepared, err := p.(RequestPreparer).PrepareRequest(history, nil, true)
		if err != nil {
			b.Fatal(err)
		}
		footprint, err := prepared.RequestFootprint()
		if err != nil || footprint.VisualTokens <= 0 || footprint.WireBytes <= int64(encoded.Len()) {
			b.Fatalf("image budget missing: %+v %v", footprint, err)
		}
	}
}
