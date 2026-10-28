package message

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderStateRoundTripPreservesOldMessageFormat(t *testing.T) {
	legacy := []byte(`[{"type":"text","data":{"text":"hello"}},{"type":"finish","data":{"reason":"end_turn","time":123}}]`)
	parts, err := unmarshallParts(legacy)
	require.NoError(t, err)
	msg := Message{Role: Assistant, Parts: parts}
	require.Nil(t, msg.FinishPart().ProviderState)
	state := &ProviderState{Provider: "openai", Model: "test-model", API: "responses", Data: json.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque"}]`)}
	msg.SetProviderState(state)
	msg.AddFinish(FinishReasonPermissionDenied)
	raw, err := marshallParts(msg.Parts)
	require.NoError(t, err)
	parts, err = unmarshallParts(raw)
	require.NoError(t, err)
	loaded := Message{Role: Assistant, Parts: parts}
	require.Equal(t, state, loaded.FinishPart().ProviderState)
	require.Equal(t, "hello", loaded.Content().String())
	// The optional field remains readable by older binaries, which ignore it.
	var oldReader []struct {
		Type string `json:"type"`
		Data struct {
			Text   string `json:"text"`
			Reason string `json:"reason"`
			Time   int64  `json:"time"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &oldReader))
	require.Equal(t, "hello", oldReader[0].Data.Text)
	require.Equal(t, "permission_denied", oldReader[1].Data.Reason)
}
