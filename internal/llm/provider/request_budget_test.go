package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

type footprintTool struct{}

func (footprintTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: "view", Description: "Read a file", Parameters: map[string]any{"file_path": map[string]any{"type": "string"}}, Required: []string{"file_path"}}
}
func (footprintTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	panic("sizing must never execute tools")
}
func TestFootprintIncludesOllamaPrivateSchemaAndSystemSuffix(t *testing.T) {
	p, err := NewProvider(models.ProviderLocal, WithBaseURL("http://127.0.0.1:11434/v1"), WithModel(models.Model{Provider: models.ProviderLocal, API: "ollama", APIModel: "llama3.1:8b", ContextWindow: 16384}), WithSystemMessage("actual system"), WithMaxTokens(2048))
	require.NoError(t, err)
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Explique em português"}}}}
	ts := []tools.BaseTool{footprintTool{}}
	footprint, err := p.(RequestSizer).RequestFootprint(history, ts)
	require.NoError(t, err)
	require.True(t, footprint.ExactSerialization)
	require.EqualValues(t, 2048, footprint.ReservedOutputTokens)
	client := p.(*baseProvider).client.(*ollamaClient)
	_, payload, err := client.request(history, ts, true)
	require.NoError(t, err)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Contains(t, string(raw), "final_answer")
	require.EqualValues(t, len(raw), footprint.ASCIIBytes+footprint.OtherBytes)
	require.Positive(t, footprint.OtherBytes)
	require.Positive(t, footprint.SchemaASCIIBytes)
}
func TestNamedLocalProfileDoesNotUseLegacyEndpointEnvironment(t *testing.T) {
	t.Setenv("LOCAL_ENDPOINT", "http://127.0.0.1:9191/v1")
	for _, profile := range []string{"", "studio"} {
		p, err := NewProvider(models.ProviderLocal, WithBaseURL("http://127.0.0.1:9292/v1"), WithModel(models.Model{Provider: models.ProviderLocal, ProviderProfile: profile, API: "ollama", APIModel: "selected"}))
		require.NoError(t, err)
		base := p.(*baseProvider).client.(*ollamaClient).transport.baseURL
		if profile == "" {
			require.Equal(t, "http://127.0.0.1:9191", base)
		} else {
			require.Equal(t, "http://127.0.0.1:9292", base)
		}
	}
}
