package agent

import (
	"fmt"
	"slices"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
)

type QueuedInstruction struct {
	ID          string
	Content     string
	Attachments []message.Attachment
}
type QueueControl interface {
	QueuedInstructions(sessionID string) []QueuedInstruction
	TakeInstruction(sessionID, instructionID string) (QueuedInstruction, error)
}

func publicInstruction(in instruction) QueuedInstruction {
	result := QueuedInstruction{ID: in.id, Content: in.content}
	for _, attachment := range in.attachments {
		attachment.Content = slices.Clone(attachment.Content)
		result.Attachments = append(result.Attachments, attachment)
	}

	return result
}
func (a *agent) QueuedInstructions(session string) []QueuedInstruction {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	var result []QueuedInstruction
	if run := a.runs[session]; run != nil {
		for _, item := range run.pending {
			result = append(result, publicInstruction(item))
		}
	}
	return result
}
func (a *agent) TakeInstruction(session, id string) (QueuedInstruction, error) {
	a.runMu.Lock()
	run := a.runs[session]
	if run != nil {
		for i, item := range run.pending {
			if item.id == id {
				run.pending = slices.Delete(run.pending, i, i+1)
				run.queuedBytes -= item.bytes
				a.runMu.Unlock()
				a.Publish(pubsub.UpdatedEvent, AgentEvent{Type: AgentEventTypeInstructions, SessionID: session, Progress: "Queued instruction removed for editing"})
				return publicInstruction(item), nil
			}
		}
	}
	a.runMu.Unlock()
	return QueuedInstruction{}, fmt.Errorf("instruction already consumed or no longer queued")
}
