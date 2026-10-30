package message

import (
	"context"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/db"
	"github.com/stretchr/testify/require"
)

type streamQueries struct {
	eventQueries
	writes int
}

func (q *streamQueries) UpdateMessage(context.Context, db.UpdateMessageParams) error {
	q.writes++
	return nil
}

func TestStreamSnapshotsDoNotWriteOrMutatePriorEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &streamQueries{}
	store := NewService(q)
	events := store.Subscribe(ctx)
	msg := Message{ID: "stream", Role: Assistant, Parts: []ContentPart{TextContent{Text: "prefix", Origin: "application"}}}
	buffer := NewStreamBuffer(&msg)
	buffer.AppendText(" first")
	buffer.AppendReasoning("thought")
	buffer.Materialize()
	store.(StreamPublisher).PublishUpdate(msg)
	first := (<-events).Payload
	for range 2048 {
		buffer.AppendText(strings.Repeat("x", 32))
	}
	buffer.AppendReasoning(" more")
	buffer.Materialize()
	store.(StreamPublisher).PublishUpdate(msg)
	second := (<-events).Payload
	require.Zero(t, q.writes)
	require.Equal(t, "prefix first", first.Content().Text)
	require.Equal(t, "application", first.Content().Origin)
	require.Equal(t, "thought", first.ReasoningContent().Thinking)
	require.Equal(t, "thought more", second.ReasoningContent().Thinking)
	require.Len(t, second.Content().Text, len("prefix first")+64<<10)
	require.NoError(t, store.Update(ctx, msg))
	require.Equal(t, 1, q.writes)
}

func TestStreamBufferMatchesAppendSemanticsAndPreservesOtherParts(t *testing.T) {
	msg := Message{Parts: []ContentPart{TextContent{Text: "a", Origin: "application"}, ToolCall{ID: "call", Origin: "application"}, TextContent{Text: "b"}, ReasoningContent{Thinking: "r"}}}
	buffer := NewStreamBuffer(&msg)
	buffer.AppendText("!")
	buffer.AppendReasoning("?")
	msg.Parts = append(msg.Parts, ToolCall{ID: "later", Finished: true})
	buffer.Materialize()
	require.Equal(t, TextContent{Text: "a!", Origin: "application"}, msg.Parts[0])
	require.Equal(t, TextContent{Text: "b!"}, msg.Parts[2])
	require.Equal(t, ReasoningContent{Thinking: "r?"}, msg.Parts[3])
	require.Equal(t, "application", msg.ToolCalls()[0].Origin)
	require.False(t, msg.ToolCalls()[0].Finished)
	require.True(t, msg.ToolCalls()[1].Finished)
}

var benchmarkStreamJSON []byte
var benchmarkStreamSnapshot Message

// Baseline mirrors the audited path: AppendContent + full JSON per delta.
// Checkpoints retains identical final JSON and immutable live snapshots, with
// the same 64KiB / 2048 fragments. SQL itself is excluded in both variants.
func BenchmarkStreamAccumulation(b *testing.B) {
	for _, checkpointed := range []bool{false, true} {
		name := "Baseline"
		if checkpointed {
			name = "Checkpoints"
		}
		b.Run(name, func(b *testing.B) { benchmarkStreamAccumulation(b, checkpointed) })
	}
}

func benchmarkStreamAccumulation(b *testing.B, checkpointed bool) {
	delta := strings.Repeat("x", 32)
	b.ReportAllocs()
	b.SetBytes(64 << 10)
	for b.Loop() {
		msg := Message{Role: Assistant}
		buffer := NewStreamBuffer(&msg)
		for i := range 2048 {
			if !checkpointed {
				msg.AppendContent(delta)
				benchmarkStreamJSON, _ = marshallParts(msg.Parts)
				continue
			}
			buffer.AppendText(delta)
			if (i+1)%128 == 0 {
				buffer.Materialize()
				benchmarkStreamSnapshot = snapshotMessage(msg)
			}
			if (i+1)%512 == 0 {
				benchmarkStreamJSON, _ = marshallParts(msg.Parts)
			}
		}
	}
}
