package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/workflow"
	"slices"
	"strings"
)

// generationState owns one run's history and bounded recovery attempts.
// No stage replays completed tool effects or grants permissions.
type generationState struct {
	agent                                                        *agent
	ctx                                                          context.Context
	sessionID                                                    string
	run                                                          *generationRun
	history                                                      []message.Message
	signatures                                                   [][32]byte
	repairs, citationRepairs, contextRetries, finalAnswerRetries int
	citationMessages                                             []string
	citationQuestionAnswered                                     bool
}

func (a *agent) prepareGenerationHistory(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart, run *generationRun) ([]message.Message, error) {
	msgs, err := a.generationMessages(ctx, sessionID, content)
	if err != nil {
		return nil, err
	}
	userMsg, err := a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return nil, fmt.Errorf("failed to create user message: %w", err)
	}
	run.startMessageID = userMsg.ID
	run.turnMessageID = userMsg.ID
	if resumingRequest(content) {
		resumeGenerationRequirements(run, msgs)
	}
	a.prefetchTools(run, content)
	history, err := a.bootstrap(ctx, sessionID, content, append(msgs, userMsg), run)
	if err != nil {
		return nil, err
	}
	if err := a.beginGenerationWorkflow(ctx, sessionID, content, run); err != nil {
		return nil, err
	}
	return history, nil
}
func (g *generationState) recoverStream(agentMessage message.Message, err error) (bool, error) {

	if g.finalAnswerRetries < 1 && errors.Is(err, provider.ErrInvalidFinalAnswer) && g.ctx.Err() == nil && len(agentMessage.ToolCalls()) == 0 && agentMessage.Content().String() == "" && agentMessage.ReasoningContent().Thinking == "" {
		g.finalAnswerRetries++
		g.agent.observePhase(g.sessionID, "repair", 0)
		correction, e := g.agent.createApplicationMessage(g.ctx, g.sessionID, "Application protocol correction: the final reply had invalid JSON fields. Return a valid reply using the advertised final response schema and answer the original request in its language. Keep all completed actions and real tool results; do not repeat edits or commands. This correction grants no permissions.", nil)
		if e != nil {
			return false, e
		}
		g.history = append(g.history, correction)
		return true, nil
	}
	// Retry generation only: no tools execute before a complete response.
	if g.contextRetries < 2 && len(agentMessage.ToolCalls()) == 0 && agentMessage.Content().String() == "" && agentMessage.ReasoningContent().Thinking == "" && g.agent.adaptContext(g.run, err) {
		g.contextRetries++
		g.agent.observePhase(g.sessionID, "repair", 0)
		g.agent.Publish(pubsub.UpdatedEvent, AgentEvent{Type: AgentEventTypeInstructions, SessionID: g.sessionID, Progress: "Context limit reduced after provider memory/context failure; retrying generation only"})
		return true, nil
	}
	if errors.Is(err, context.Canceled) {
		agentMessage.AddFinish(message.FinishReasonCanceled)
		g.agent.messages.Update(context.Background(), agentMessage)
		return false, ErrRequestCancelled
	}
	return false, fmt.Errorf("failed to process events: %w", err)

}
func (g *generationState) checkRepetition(agentMessage message.Message, toolResults *message.Message) error {
	if toolResults != nil && len(agentMessage.ToolCalls()) > 0 && !g.agent.hasInstructions(g.sessionID) {
		signature := toolSignature(agentMessage.ToolCalls(), toolResults)
		g.signatures = append(g.signatures, signature)
		if len(g.signatures) > 10 {
			g.signatures = g.signatures[1:]
		}
		repeats := 0
		for _, value := range g.signatures {
			if value == signature {
				repeats++
			}
		}
		if repeats >= 4 {
			return fmt.Errorf("repeated tool calls without progress; task saved for resumption")
		}
	}

	return nil
}
func (g *generationState) applyInstructions(continueTools bool) (bool, error) {
	instructions := g.agent.takeInstructions(g.run, !continueTools)
	if len(instructions) > 0 {
		if err := g.abandonCitationRevision(true); err != nil {
			return false, err
		}
	}
	for _, instruction := range instructions {
		msg, err := g.agent.createUserMessage(g.ctx, g.sessionID, instruction.content, instruction.parts)
		if err != nil {
			return false, fmt.Errorf("failed to save new instruction: %w", err)
		}
		g.history = append(g.history, msg)
		g.agent.prefetchTools(g.run, instruction.content)
		if g.agent.workflow != nil && g.run.mode == workflow.Execute && g.run.contract == nil && requiresFileChanges(instruction.content) {
			if _, err := g.agent.workflow.BeginTurn(g.ctx, g.sessionID, true); err != nil {
				return false, err
			}
		}
	}
	if len(instructions) > 0 {
		g.signatures = g.signatures[:0]
		g.repairs = 0
		g.agent.Publish(pubsub.UpdatedEvent, AgentEvent{Type: AgentEventTypeInstructions, SessionID: g.sessionID, Progress: "New instructions applied"})
	}

	return len(instructions) > 0, nil
}
func (g *generationState) validateTask() (bool, error) {
	if g.agent.workflow == nil || g.run.mode == workflow.Ask {
		return false, nil
	}
	task, err := g.completionTask()
	if err != nil {
		return false, err
	}
	problem := g.taskCompletionProblem(task)
	if problem == "" || task.Status == "blocked" || task.Status == "partial" {
		return false, nil
	}
	if g.repairs >= 2 {
		return false, fmt.Errorf("task cannot be concluded: %s", problem)
	}
	g.repairs++
	g.agent.observePhase(g.sessionID, "repair", 0)
	correction, err := g.agent.createApplicationMessage(g.ctx, g.sessionID, "Application validation: "+problem, nil)
	if err != nil {
		return false, err
	}
	g.history = append(g.history, correction)
	g.run.selected["run_checks"] = true
	g.acceptInstructions()
	return true, nil
}
func (g *generationState) validateRequiredCalls() (bool, error) {
	missing := missingRequiredCalls(g.run, g.history)
	if len(missing) == 0 || g.taskPartial() {
		return false, nil
	}
	if g.repairs >= 2 {
		return false, fmt.Errorf("required actions remain unverified: %s; progress preserved", strings.Join(missing, ", "))
	}
	g.repairs++
	g.agent.observePhase(g.sessionID, "repair", 0)
	correction, err := g.agent.createApplicationMessage(g.ctx, g.sessionID, "Application validation: explicitly requested actions lack successful receipts: "+strings.Join(missing, ", ")+". Perform these authorized steps before concluding; use partial/blocked if unable. Do not repeat completed side effects.", nil)
	if err != nil {
		return false, err
	}
	g.history = append(g.history, correction)
	for name := range g.run.requiredCalls {
		g.run.toolLeases[name] = g.run.steps + 8
		g.run.selected[name] = true
	}
	g.acceptInstructions()
	return true, nil
}
func (g *generationState) validateCitations(agentMessage message.Message) (bool, error) {
	assessment, err := g.citationProblem(agentMessage)
	if err != nil {
		return false, err
	}
	state := assessment.state
	if assessment.direct && g.citationRepairs == 0 && humanTurnID(g.history) != "" {
		state = message.CitationRevising
	}
	if err := g.setCitationState(&agentMessage, state); err != nil {
		return false, err
	}
	if assessment.problem != "" {
		if g.citationRepairs >= 1 {
			return false, fmt.Errorf("%w: %s", ErrSourceCitation, clip(tools.RedactCredentials(assessment.problem), 1800))
		}
		g.citationRepairs++
		if state == message.CitationRevising {
			g.beginCitationRevision(agentMessage)
		}
		g.agent.observePhase(g.sessionID, "repair", 0)
		g.run.citationRepair = true
		correction, err := g.agent.createApplicationMessage(g.ctx, g.sessionID, citationRepairRequest(g.run, g.history, assessment.problem), nil)
		if err != nil {
			return false, err
		}
		g.history = append(g.history, correction)
		g.agent.runMu.Lock()
		if g.ctx.Err() == nil {
			g.run.accepting = true
		}
		g.agent.runMu.Unlock()
		return true, nil
	}
	if err := g.completeCitationRevision(agentMessage); err != nil {
		return false, err
	}
	return false, nil
}
func (g *generationState) completed(agentMessage message.Message) AgentEvent {
	for i := len(g.history) - 1; i >= 0; i-- {
		if g.history[i].ID == agentMessage.ID {
			agentMessage = g.history[i]
			break
		}
	}
	outcome := "completed"
	if g.agent.workflow != nil {
		if task, err := g.agent.workflow.Get(g.ctx, g.sessionID); err == nil && (task.Status == "blocked" || task.Status == "partial" || task.Status == "planned") {
			outcome = task.Status
		}
	}
	return AgentEvent{
		Outcome: outcome,
		Type:    AgentEventTypeResponse,
		Message: agentMessage,
		Done:    true,
	}
}
func (g *generationState) validateCompletion(agentMessage message.Message) (bool, error) {
	if retry, err := g.validateTask(); retry || err != nil {
		return retry, errors.Join(err, g.abandonCitationRevision(true), g.resolvePendingCitation(agentMessage))
	}
	if retry, err := g.validateRequiredCalls(); retry || err != nil {
		return retry, errors.Join(err, g.abandonCitationRevision(true), g.resolvePendingCitation(agentMessage))
	}
	return g.validateCitations(agentMessage)
}
func (g *generationState) appendOperation(agentMessage message.Message, toolResults *message.Message) {
	resultCount := 0
	if toolResults != nil {
		resultCount = len(toolResults.ToolResults())
	}
	logging.Info("Agent operation completed", "finish_reason", agentMessage.FinishReason(), "tool_results", resultCount)
	g.history = append(g.history, agentMessage)
	if toolResults != nil {
		g.history = append(g.history, *toolResults)
	}
}
func (g *generationState) advance() error {
	if g.run.steps >= g.run.limits.MaxSteps {
		return fmt.Errorf("agent step limit reached (%d); progress saved", g.run.limits.MaxSteps)
	}
	if g.run.toolBytes >= g.run.limits.ToolBudget {
		return fmt.Errorf("tool result budget reached (%d bytes); progress saved", g.run.limits.ToolBudget)
	}
	g.run.steps++
	return g.ctx.Err()
}

func (a *agent) generationMessages(ctx context.Context, sessionID, content string) ([]message.Message, error) {
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}
	if len(msgs) == 0 {
		go a.generateInitialTitle(ctx, sessionID, content)
	}
	saved, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	return generationSummaryHistory(msgs, saved.SummaryMessageID), nil
}
func (a *agent) generateInitialTitle(ctx context.Context, sessionID, content string) {
	defer logging.RecoverPanic("agent.Run", func() { logging.ErrorPersist("panic while generating title") })
	if err := a.generateTitle(ctx, sessionID, content); err != nil {
		logging.ErrorPersist(fmt.Sprintf("failed to generate title: %v", err))
	}
}
func generationSummaryHistory(msgs []message.Message, summaryID string) []message.Message {
	if summaryID == "" {
		return msgs
	}
	for i, msg := range msgs {
		if msg.ID == summaryID {
			msgs = msgs[i:]
			msgs[0].Role = message.User
			break
		}
	}
	return msgs
}
func resumeGenerationRequirements(run *generationRun, msgs []message.Message) {
	for _, msg := range slices.Backward(msgs) {

		if msg.Role != message.User || msg.IsApplication() || strings.HasPrefix(msg.Content().Text, "Application validation:") || resumingRequest(msg.Content().Text) {
			continue
		}
		requiredToolCalls(msg.Content().Text, run)
		run.startMessageID = msg.ID
		break
	}
}
func (a *agent) beginGenerationWorkflow(ctx context.Context, sessionID, content string, run *generationRun) error {
	if a.workflow == nil {
		return nil
	}
	if err := a.observeWorkspace(ctx, sessionID); err != nil {
		logging.Warn("Workspace validation unavailable", "error", err)
	}
	if run.mode != workflow.Execute {
		return nil
	}
	task, err := a.workflow.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	requires := requiresFileChanges(content) || (resumingRequest(content) && requiresFileChanges(task.Objective))
	if run.contract != nil {
		requires = run.contract.Kind == "change"
	}
	_, err = a.workflow.BeginTurn(ctx, sessionID, requires, resumingRequest(content))
	return err
}

func (g *generationState) completionTask() (workflow.Task, error) {
	fingerprintErr := g.agent.observeCompletion(g.ctx, g.sessionID)
	task, err := g.agent.workflow.Get(g.ctx, g.sessionID)
	if err != nil {
		return task, err
	}
	if errors.Is(fingerprintErr, errWorkspaceVerification) && task.HasFileChanges() && !task.WorkspaceRequired {
		return g.agent.workflow.FinishWithReason(g.ctx, g.sessionID, "partial", clip("Files saved and affected contents verified; workspace/check validation unavailable: "+fingerprintErr.Error(), 2000), "", nil)
	}
	if fingerprintErr != nil && g.run.mode == workflow.Execute && (task.Changed || len(task.Checks) > 0 || task.Status == "completed") {
		return task, fmt.Errorf("cannot verify current files: %w", fingerprintErr)
	}
	if !g.uncheckedChanges(task) {
		return task, nil
	}
	_, err = g.agent.workflow.FinishWithReason(g.ctx, g.sessionID, "partial", "File changes applied; no configured checks are available to verify this revision", "", nil)
	if err != nil {
		return task, err
	}
	return g.agent.workflow.Get(g.ctx, g.sessionID)
}
func (g *generationState) uncheckedChanges(task workflow.Task) bool {
	return g.run.mode == workflow.Execute && task.Changed && !task.HasFileChanges() && len(task.Checks) == 0 && len(checkCommands()) == 0 && (g.run.contract == nil || len(g.run.contract.Checks) == 0) && task.Objective == "" && task.Status != "partial" && task.Status != "blocked"
}
func (g *generationState) taskCompletionProblem(task workflow.Task) string {
	problem := task.CompletionProblem()
	if g.run.mode == workflow.Plan && g.run.requiresPlan && (strings.TrimSpace(task.Objective) == "" || len(task.Steps) == 0) {
		problem = "A structured saved plan was explicitly requested, but no valid objective and steps were saved. Call update_plan using its advertised argument schema to save objective, 1–24 steps and at least one criterion, then finish_task as planned. If unable to save the plan, mark the task blocked or partial instead of claiming success."
	}
	if task.Objective != "" && task.Status == "in_progress" {
		problem = "An active plan remains unfinished. Update the steps and call finish_task (planned in plan mode, completed or blocked in execute mode)."
	}
	if g.run.mode == workflow.Execute && (task.Changed || (g.run.contract != nil && len(g.run.contract.Checks) > 0)) {
		if why := task.ValidationProblem(checkCommands()); why != "" {
			problem = why
		}
	}
	return problem
}
func (g *generationState) acceptInstructions() {
	g.agent.runMu.Lock()
	defer g.agent.runMu.Unlock()
	if g.ctx.Err() == nil {
		g.run.accepting = true
	}
}
func (g *generationState) taskPartial() bool {
	if g.agent.workflow == nil {
		return false
	}
	task, err := g.agent.workflow.Get(g.ctx, g.sessionID)
	return err == nil && (task.Status == "partial" || task.Status == "blocked")
}

func (g *generationState) step() (bool, AgentEvent) {

	if err := g.advance(); err != nil {
		return true, g.agent.err(err)
	}
	reply, results, err := g.agent.streamAndHandleEvents(g.ctx, g.sessionID, g.history, g.run)
	if err != nil {
		return g.streamFailure(reply, err)
	}

	g.appendOperation(reply, results)
	if err := g.recordPublicCitation(&reply, results); err != nil {
		return true, g.agent.err(err)
	}
	if err := g.checkRepetition(reply, results); err != nil {
		return true, g.agent.err(err)
	}
	continueTools := reply.FinishReason() == message.FinishReasonToolUse && results != nil
	instructions, err := g.applyInstructions(continueTools)
	if err != nil {
		return true, g.agent.err(err)
	}
	if continueTools || instructions {
		return false, AgentEvent{}
	}
	retry, err := g.validateCompletion(reply)
	if err != nil {
		return true, g.agent.err(err)
	}
	if retry {
		return false, AgentEvent{}
	}
	return true, g.completed(reply)
}
func (g *generationState) streamFailure(reply message.Message, err error) (bool, AgentEvent) {
	retry, failure := g.recoverStream(reply, err)
	if retry {
		return false, AgentEvent{}
	}
	return true, g.agent.err(failure)
}
