package agent

import (
	"context"
	"time"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/message"
)

const (
	streamPublishInterval    = 50 * time.Millisecond
	streamCheckpointInterval = time.Second
	streamPublishBytes       = 4 << 10
	streamCheckpointBytes    = 16 << 10
)

// One worker owns the buffer and all flushes. SQL checkpoints trigger on the
// next event after one second, at 16KiB, and at stream boundaries. There is no
// independent timer while the provider is idle; UI snapshots are independent.
type streamMessageWriter struct {
	service                     message.Service
	msg                         *message.Message
	buffer                      *message.StreamBuffer
	unpublished, unpersisted    int
	lastPublish, lastCheckpoint time.Time
}

func newStreamMessageWriter(service message.Service, msg *message.Message, now time.Time) *streamMessageWriter {
	return &streamMessageWriter{service: service, msg: msg, buffer: message.NewStreamBuffer(msg), lastCheckpoint: now}
}

func (w *streamMessageWriter) append(event provider.ProviderEvent) bool {
	switch event.Type {
	case provider.EventContentDelta:
		w.buffer.AppendText(event.Content)
	case provider.EventThinkingDelta:
		w.buffer.AppendReasoning(event.Content)
	default:
		return false
	}
	w.unpublished += len(event.Content)
	w.unpersisted += len(event.Content)
	return true
}

func (w *streamMessageWriter) flush(ctx context.Context, now time.Time, force bool) error {
	checkpoint := w.unpersisted > 0 && (force || w.unpersisted >= streamCheckpointBytes || now.Sub(w.lastCheckpoint) >= streamCheckpointInterval)
	publish := w.unpublished > 0 && (w.lastPublish.IsZero() || w.unpublished >= streamPublishBytes || now.Sub(w.lastPublish) >= streamPublishInterval)
	if !checkpoint && !publish {
		return nil
	}
	w.materialize()
	if checkpoint {
		if err := w.service.Update(ctx, *w.msg); err != nil {
			return err
		}
		w.unpersisted, w.unpublished = 0, 0
		w.lastCheckpoint, w.lastPublish = now, now
		return nil
	}
	if publisher, ok := w.service.(message.StreamPublisher); ok {
		publisher.PublishUpdate(*w.msg)
	} else if err := w.service.Update(ctx, *w.msg); err != nil {
		return err
	}
	w.unpublished, w.lastPublish = 0, now
	return nil
}

func (w *streamMessageWriter) handle(ctx context.Context, event provider.ProviderEvent) error {
	if w.append(event) {
		return w.flush(ctx, time.Now(), false)
	}
	return w.flush(ctx, time.Now(), true)
}

func (w *streamMessageWriter) materialize() { w.buffer.Materialize() }
