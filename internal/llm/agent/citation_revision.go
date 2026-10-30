package agent

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type citationRevision struct {
	target message.Message
	turnID string
}

func humanTurnID(history []message.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == message.User && !history[i].IsApplication() {
			return history[i].ID
		}
	}
	return ""
}

func (g *generationState) beginCitationRevision(item message.Message) {
	turnID := humanTurnID(g.history)
	if turnID == "" || len(item.ToolCalls()) > 0 {
		return
	}
	item.Parts = slices.Clone(item.Parts)
	g.run.citationRevisionOf = item.ID
	g.run.citationRevision = &citationRevision{target: item, turnID: turnID}
}

func citationRevisionParts(run *generationRun, history []message.Message) []message.ContentPart {
	if revision := run.citationRevision; revision != nil && revision.turnID == humanTurnID(history) {
		return []message.ContentPart{message.TextContent{RevisionOf: run.citationRevisionOf, CitationStatus: message.CitationPending}}
	}
	return nil
}

func (a *agent) saveCitationMessage(ctx context.Context, item message.Message) error {
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
	}
	return a.messages.Update(ctx, item)
}

func (g *generationState) persistCitationMessage(item message.Message) error {
	if err := g.agent.saveCitationMessage(g.ctx, item); err != nil {
		return err
	}
	for i := range g.history {
		if g.history[i].ID == item.ID {
			g.history[i] = item
			break
		}
	}
	return nil
}

func (g *generationState) abandonCitationRevision(clearLinks bool) error {
	revision := g.run.citationRevision
	if revision == nil {
		return nil
	}
	var linkError error
	if clearLinks {
		linkError = g.detachCitationCandidates(revision.target.ID)
	}
	failure := g.setCitationState(&revision.target, message.CitationInvalid)
	if failure == nil {
		g.run.citationRevisionOf, g.run.citationRevision = "", nil
	}
	return errors.Join(linkError, failure)
}

func (g *generationState) detachCitationCandidates(targetID string) error {
	var failures []error
	for _, item := range g.history {
		if item.RevisionOf() == targetID {
			if err := g.detachCitationCandidate(item); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (g *generationState) detachCitationCandidate(item message.Message) error {
	item.Parts = slices.Clone(item.Parts)
	item.SetRevisionOf("")
	if err := g.persistCitationMessage(item); err != nil {
		return err
	}
	return g.resolvePendingCitation(item)
}

func (g *generationState) completeCitationRevision(item message.Message) error {
	revision := g.run.citationRevision
	if revision == nil {
		return nil
	}
	if item.RevisionOf() != revision.target.ID || item.CitationValidation() != message.CitationValid || len(item.ToolCalls()) > 0 || revision.turnID != humanTurnID(g.history) {
		return g.abandonCitationRevision(true)
	}
	if item.FinishReason() != message.FinishReasonEndTurn {
		return g.abandonCitationRevision(false)
	}
	// The verified candidate is durable before its original changes presentation.
	if err := g.setCitationState(&revision.target, message.CitationSuperseded); err != nil {
		return err
	}
	g.run.citationRevisionOf, g.run.citationRevision = "", nil
	return nil
}

func (a *agent) detachToolRevision(ctx context.Context, history []message.Message, run *generationRun, item *message.Message) error {
	if len(item.ToolCalls()) == 0 || item.RevisionOf() == "" {
		return nil
	}
	item.Parts = slices.Clone(item.Parts)
	item.SetRevisionOf("")
	if err := a.saveCitationMessage(ctx, *item); err != nil {
		return err
	}
	revision := run.citationRevision
	if revision == nil {
		return nil
	}
	if citationReadOnlyBatch(run, item.ToolCalls()) {
		return nil
	}
	target := revision.target
	target.Parts = slices.Clone(target.Parts)
	target.SetCitationValidation(message.CitationInvalid)
	if err := a.saveCitationMessage(ctx, target); err != nil {
		return err
	}
	for i := range history {
		if history[i].ID == revision.target.ID {
			history[i] = target
			break
		}
	}
	run.citationRevisionOf, run.citationRevision = "", nil
	return nil
}

func citationReadOnlyBatch(run *generationRun, calls []message.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !nativeCitationReadTool(run, call.Name) {
			return false
		}
	}
	return true
}

func nativeCitationReadTool(run *generationRun, name string) bool {
	if !nativeSourceReadToolName(name) {
		return false
	}
	for _, tool := range run.available {
		if tool.Info().Name == name {
			return !tools.IsOpaqueTool(tool)
		}
	}
	return false
}
