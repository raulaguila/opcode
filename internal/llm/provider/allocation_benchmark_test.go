package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
)

// Keep this fixture compatible with the prior implementation for paired runs.
func BenchmarkAuditKnownTokenizer(b *testing.B) {
	for _, id := range []string{"fixture", "gpt-4o"} {
		for _, changing := range []bool{false, true} {
			mode := "stable"
			if changing {
				mode = "changing"
			}
			b.Run(id+"/"+mode, func(b *testing.B) { benchmarkKnownTokenizer(b, id, changing) })
		}
	}
}

func benchmarkKnownTokenizer(b *testing.B, id string, changing bool) {
	history := make([]message.Message, 20)
	text := strings.Repeat("text ", 1600)
	for i := range history {
		history[i] = message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}
	}
	model := models.Model{Provider: models.ProviderOpenAI, APIModel: id, ContextWindow: 1000000, DefaultMaxTokens: 2048}
	p, err := NewProvider(model.Provider, WithModel(model), WithAPIKey("audit-fixture"), WithMaxTokens(2048))
	if err != nil {
		b.Fatal(err)
	}
	prepare := p.(RequestPreparer)
	if _, err = prepare.PrepareRequest(history, nil, true); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(text) * len(history)))
	b.ResetTimer()
	for i := range b.N {
		if changing {
			history[len(history)-1].Parts = []message.ContentPart{message.TextContent{Text: fmt.Sprintf("%s request-%d", text, i)}}
		}
		if _, err = prepare.PrepareRequest(history, nil, true); err != nil {
			b.Fatal(err)
		}
	}
}
