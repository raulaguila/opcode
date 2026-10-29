package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/review"
	"github.com/raulaguila/opcode/internal/workflow"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
)

type instruction struct {
	id          string
	content     string
	parts       []message.ContentPart
	attachments []message.Attachment
	bytes       int
}

type generationRun struct {
	contract                *workflow.Contract
	ctx                     context.Context
	cancel                  context.CancelFunc
	done                    chan struct{}
	accepting               bool
	pending                 []instruction
	queuedBytes             int
	mode                    workflow.Mode
	selected                map[string]bool
	toolLeases              map[string]int
	requiredCalls           map[string]int
	startMessageID          string
	turnMessageID           string
	available               []tools.BaseTool
	limits                  config.RuntimeConfig
	steps, toolBytes        int
	hookRuns                int
	provider                provider.Provider
	requiresPlan            bool
	requiresCitation        bool
	requiresProjectEvidence bool
	primaryEvidence         map[string]bool
	citationRepair          bool
}

func (a *agent) Run(ctx context.Context, sessionID, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	events, _, err := a.submit(ctx, sessionID, content, false, attachments...)
	return events, err
}

// Submit starts a turn or queues instructions for its next safe boundary. Only
// the worker persists them, after any outstanding tool results, preserving the
// provider's assistant/tool/user ordering and single-writer conversation state.
func (a *agent) Submit(ctx context.Context, sessionID, content string, attachments ...message.Attachment) (<-chan AgentEvent, bool, error) {
	return a.submit(ctx, sessionID, content, true, attachments...)
}

func (a *agent) submit(ctx context.Context, sessionID, content string, allowQueue bool, attachments ...message.Attachment) (<-chan AgentEvent, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if sessionID == "" || (content == "" && len(attachments) == 0) {
		return nil, false, fmt.Errorf("session and message content are required")
	}
	incoming := instruction{id: uuid.NewString(), content: content, bytes: len(content)}
	if a.Model().SupportsAttachments {
		for _, attachment := range attachments {
			incoming.bytes += len(attachment.Content)
			copyAttachment := attachment
			copyAttachment.Content = append([]byte(nil), attachment.Content...)
			incoming.attachments = append(incoming.attachments, copyAttachment)
			incoming.parts = append(incoming.parts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: append([]byte(nil), attachment.Content...)})
		}
	}
	for {
		a.runMu.Lock()
		if err := ctx.Err(); err != nil {
			a.runMu.Unlock()
			return nil, false, err
		}
		if a.closed {
			a.runMu.Unlock()
			return nil, false, fmt.Errorf("agent is closed")
		}
		if current := a.runs[sessionID]; current != nil {
			if !allowQueue {
				a.runMu.Unlock()
				return nil, false, ErrSessionBusy
			}
			if !current.accepting {
				a.runMu.Unlock()
				return nil, false, ErrSessionBusy
			}
			if len(current.pending) >= 8 || current.queuedBytes+incoming.bytes > 1<<20 {
				a.runMu.Unlock()
				return nil, false, fmt.Errorf("instruction queue is full (8 messages or 1 MiB); wait for the current operation")
			}
			current.pending = append(current.pending, incoming)
			current.queuedBytes += incoming.bytes
			count := len(current.pending)
			a.runMu.Unlock()
			a.Publish(pubsub.UpdatedEvent, AgentEvent{Type: AgentEventTypeInstructions, SessionID: sessionID, Progress: fmt.Sprintf("%d instruction(s) queued for the next operation", count)})
			return nil, true, nil
		}
		if a.IsSessionBusy(sessionID) {
			a.runMu.Unlock()
			return nil, false, ErrSessionBusy
		}
		limits := runtimeConfig()
		genCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.MaxSeconds)*time.Second)
		turnID := uuid.NewString()
		genCtx = tools.WithChangeObserver(genCtx, func(changes []tools.FileChange) error {
			if cfg := config.Get(); cfg != nil {
				return review.New(config.WorkingDirectory(), cfg.Data.Directory).Record(sessionID, turnID, changes)
			}
			return fmt.Errorf("review storage is unavailable")
		})
		run := &generationRun{ctx: genCtx, cancel: cancel, done: make(chan struct{}), accepting: true, limits: limits}
		if a.runs == nil {
			a.runs = make(map[string]*generationRun)
		}
		if err := a.configureRun(run, sessionID); err != nil {
			cancel()
			a.runMu.Unlock()
			return nil, false, err
		}
		a.runs[sessionID] = run
		a.activeRequests.Store(sessionID, run)
		a.runMu.Unlock()
		events := make(chan AgentEvent, 1)
		go a.runGeneration(sessionID, incoming, run, events)
		return events, false, nil
	}
}

func (a *agent) runGeneration(sessionID string, incoming instruction, run *generationRun, events chan AgentEvent) {
	result := a.err(fmt.Errorf("agent did not complete"))
	defer func() {
		if recover() != nil {
			result = a.err(fmt.Errorf("panic while running the agent"))
		}
		a.runMu.Lock()
		run.accepting = false
		pending := run.pending
		run.pending = nil
		a.runMu.Unlock()
		if result.Error != nil && !errors.Is(result.Error, context.Canceled) && !errors.Is(result.Error, ErrRequestCancelled) && len(pending) > 0 {
			saveCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			for _, incoming := range pending {
				if _, err := a.createUserMessage(saveCtx, sessionID, incoming.content, incoming.parts); err != nil {
					result.Error = errors.Join(result.Error, err)
					break
				}
			}
			cancel()
		}
		if result.Error != nil && a.workflow != nil {
			saveCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, _ = a.workflow.Finish(saveCtx, sessionID, "blocked", clip(result.Error.Error(), 1500), checkCommands())
			cancel()
		}
		a.runMu.Lock()
		run.accepting = false
		a.runMu.Unlock()
		run.cancel()
		if result.Error != nil {
			for _, tool := range a.tools {
				if closer, ok := tool.(interface{ CancelSession(string) }); ok {
					closer.CancelSession(sessionID)
				}
			}
		}
		a.runMu.Lock()
		delete(a.runs, sessionID)
		a.activeRequests.Delete(sessionID)
		a.runMu.Unlock()

		if result.Error != nil && !errors.Is(result.Error, ErrRequestCancelled) && !errors.Is(result.Error, context.Canceled) {
			logging.ErrorPersist(result.Error.Error())
		}
		result.SessionID = sessionID
		a.Publish(pubsub.CreatedEvent, result)
		events <- result
		close(events)
		close(run.done)
	}()
	result = a.processGeneration(run.ctx, sessionID, incoming.content, incoming.parts, run)
}

func (a *agent) takeInstructions(run *generationRun, finishing bool) []instruction {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if run.ctx.Err() != nil {
		run.pending = nil
		run.accepting = false
		return nil
	}
	items := run.pending
	run.pending = nil
	run.queuedBytes = 0
	if finishing && len(items) == 0 {
		run.accepting = false
	}
	return items
}

func (a *agent) hasInstructions(sessionID string) bool {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	return a.runs[sessionID] != nil && len(a.runs[sessionID].pending) > 0
}
