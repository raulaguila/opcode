package agent

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

type attachmentProvider struct{ *controlledProvider }

func (*attachmentProvider) Model() models.Model {
	return models.Model{ID: "vision-test", SupportsAttachments: true}
}

func TestUnsupportedAttachmentsAreRejectedBeforeAcceptingOrQueueing(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new request", true: "queued instruction"}[busy], func(t *testing.T) {
			a, p, _ := steeringAgent(t)
			if busy {
				a.runs = map[string]*generationRun{"s": {ctx: context.Background(), accepting: true}}
				defer delete(a.runs, "s")
			}
			attachments := []message.Attachment{{FileName: "image.png", MimeType: "image/png", Content: []byte{1, 2, 3}}}
			events, queued, err := a.Submit(context.Background(), "s", "Review this image", attachments...)
			require.ErrorContains(t, err, "does not support attachments")
			require.Nil(t, events)
			require.False(t, queued)
			require.Empty(t, p.calls, "rejection must precede provider work")
			require.Empty(t, a.QueuedInstructions("s"))
			require.Equal(t, []byte{1, 2, 3}, attachments[0].Content)
		})
	}
}
func TestCompatibleAttachmentQueueOwnsBytesAndTextOnlyRequestsRemainValid(t *testing.T) {
	a, p, _ := steeringAgent(t)
	a.provider = &attachmentProvider{p}
	run := &generationRun{ctx: context.Background(), accepting: true}
	a.runs = map[string]*generationRun{"s": run}
	defer delete(a.runs, "s")
	image := []byte{1, 2, 3}
	_, queued, err := a.Submit(context.Background(), "s", "Review", message.Attachment{Content: image, MimeType: "image/png"})
	require.NoError(t, err)
	require.True(t, queued)
	require.Len(t, run.pending, 1)
	image[0] = 9
	require.Equal(t, []byte{1, 2, 3}, run.pending[0].attachments[0].Content)
	require.Equal(t, []byte{1, 2, 3}, run.pending[0].parts[0].(message.BinaryContent).Data)
	a.provider = p
	_, queued, err = a.Submit(context.Background(), "s", "Text without attachments")
	require.NoError(t, err)
	require.True(t, queued)
	require.Len(t, run.pending, 2)
}
