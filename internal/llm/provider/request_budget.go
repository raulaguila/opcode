package provider

import (
	"encoding/json"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

// RequestFootprint contains sizes only. It deliberately cannot expose raw
// prompts, schemas, credentials or provider continuation data to logs or UI.
type RequestFootprint struct {
	ASCIIBytes           int64
	OtherBytes           int64
	SchemaASCIIBytes     int64
	SchemaOtherBytes     int64
	ReservedOutputTokens int64
	ExactSerialization   bool
}
type RequestSizer interface {
	RequestFootprint([]message.Message, []tools.BaseTool) (RequestFootprint, error)
}

func countBytes(raw []byte) (ascii, other int64) {
	for _, b := range raw {
		if b < 128 {
			ascii++
		} else {
			other++
		}
	}
	return
}
func (p *baseProvider) RequestFootprint(history []message.Message, definitions []tools.BaseTool) (RequestFootprint, error) {
	footprint := RequestFootprint{ReservedOutputTokens: p.options.maxTokens}
	var payload any
	if protocol, ok := p.client.(interface {
		request([]message.Message, []tools.BaseTool, bool) (string, any, error)
	}); ok {
		_, value, err := protocol.request(history, definitions, true)
		if err != nil {
			return footprint, err
		}
		payload = value
		footprint.ExactSerialization = true
	} else {
		var schemas []tools.ToolInfo
		for _, tool := range definitions {
			schemas = append(schemas, tool.Info())
		}
		payload = map[string]any{"system": p.options.systemMessage, "messages": history, "tools": schemas}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return footprint, err
	}
	footprint.ASCIIBytes, footprint.OtherBytes = countBytes(raw)
	// Native HTTP adapters place schemas under tools. Gemini uses tools with
	// functionDeclarations; fallback SDK payloads use the same envelope key.
	encoded := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &encoded) == nil {
		if schemas, ok := encoded["tools"]; ok {
			footprint.SchemaASCIIBytes, footprint.SchemaOtherBytes = countBytes(schemas)
		}
	}
	return footprint, nil
}
