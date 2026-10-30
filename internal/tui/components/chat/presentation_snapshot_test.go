package chat

import (
	"encoding/json"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLivePresentationDoesNotRetainBinaryOrProviderState(t *testing.T) {
	for _, kind := range []pubsub.EventType{pubsub.CreatedEvent, pubsub.UpdatedEvent} {
		m := renderFixture(2)
		payload := m.messages[1]
		if kind == pubsub.CreatedEvent {
			payload.ID = "new-response"
		}
		payload.Parts = []message.ContentPart{message.TextContent{Text: "visible response"}, message.BinaryContent{Path: "photo.png", MIMEType: "image/png", Data: []byte("PRIVATE_BINARY")}, message.Finish{Reason: message.FinishReasonEndTurn, Time: 1, ProviderState: &message.ProviderState{Data: json.RawMessage(`{"private":"PROVIDER_STATE"}`)}}}
		m.Update(pubsub.Event[message.Message]{Type: kind, Payload: payload})
		visible := m.messages[len(m.messages)-1]
		require.Equal(t, payload.ID, visible.ID)
		require.Empty(t, visible.BinaryContent()[0].Data)
		require.Nil(t, visible.FinishPart().ProviderState)
		require.Equal(t, []byte("PRIVATE_BINARY"), payload.BinaryContent()[0].Data)
		require.NotNil(t, payload.FinishPart().ProviderState, "only UI presentation may be stripped")
	}
}
