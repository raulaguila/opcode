package message

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raulaguila/opcode/internal/db"
	"github.com/stretchr/testify/require"
)

type eventQueries struct{ db.Querier }

func (*eventQueries) UpdateMessage(context.Context, db.UpdateMessageParams) error { return nil }

func (*eventQueries) CreateMessage(_ context.Context, params db.CreateMessageParams) (db.Message, error) {
	return db.Message{ID: params.ID, SessionID: params.SessionID, Role: params.Role, Parts: params.Parts, Model: params.Model}, nil
}

func TestPublishedMessagesRemainStableDuringStreaming(t *testing.T) {
	for _, event := range []string{"created", "updated"} {
		t.Run(event, func(t *testing.T) { testPublishedMessageSnapshot(t, event == "created") })
	}
}

func testPublishedMessageSnapshot(t *testing.T, created bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := NewService(&eventQueries{})
	events := service.Subscribe(ctx)
	state := &ProviderState{Data: json.RawMessage(`{"value":1}`)}
	attachment := []byte("image")
	msg := Message{ID: "stream", Role: Assistant, Parts: []ContentPart{
		TextContent{Text: "first"},
		ToolCall{ID: "call", Name: "bash"},
		BinaryContent{Data: attachment},
		Finish{ProviderState: state},
	}}
	if created {
		var err error
		msg, err = service.Create(ctx, "session", CreateMessageParams{Role: Assistant, Parts: msg.Parts})
		require.NoError(t, err)
		attachment = msg.BinaryContent()[0].Data
		state = msg.FinishPart().ProviderState
	} else {
		require.NoError(t, service.Update(ctx, msg))
	}
	first := (<-events).Payload
	msg.AppendContent(" second")
	msg.FinishToolCall("call")
	attachment[0] = 'X'
	state.Data[len(state.Data)-2] = '2'
	require.NoError(t, service.Update(ctx, msg))
	second := (<-events).Payload

	require.Equal(t, "first", first.Content().String())
	require.False(t, first.ToolCalls()[0].Finished)
	require.Equal(t, "image", string(first.BinaryContent()[0].Data))
	require.JSONEq(t, `{"value":1}`, string(first.FinishPart().ProviderState.Data))
	require.Equal(t, "first second", second.Content().String())
	require.True(t, second.ToolCalls()[0].Finished)
}
