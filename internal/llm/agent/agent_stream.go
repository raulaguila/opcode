package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/raulaguila/opcode/internal/llm/prompt"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"time"
)

func (a *agent) prepareStreamRequest(ctx context.Context, sessionID string, msgHistory []message.Message, run *generationRun) ([]message.Message, provider.PreparedRequest, error) {
	requestHistory, requestErr := a.requestHistory(ctx, sessionID, msgHistory, run)
	if requestErr != nil {
		return nil, nil, requestErr
	}
	if controller, ok := runProvider(a, run).(provider.SystemPromptController); ok {
		names := make([]string, 0)
		for _, tool := range a.exposedTools(run) {
			names = append(names, tool.Info().Name)
		}
		model := runProvider(a, run).Model()
		next, err := controller.WithSystemPrompt(prompt.RunPrompt(string(run.mode), model.API, model.ContextWindow, names))
		if err != nil {
			return nil, nil, err
		}
		run.provider = next
	}
	if controller, ok := runProvider(a, run).(provider.SourceCitationController); ok && run.citationRepair {
		var hints []string
		if run.requiresCitation && run.citationRepair {
			hints = currentCitationChoices(run, msgHistory)
		}
		next, err := controller.WithSourceCitations(hints)
		if err != nil {
			return nil, nil, err
		}
		run.provider = next
	}
	return a.budgetPreparedRequest(sessionID, requestHistory, run)
}
func (a *agent) preserveInterruptedStream(sessionID string, assistantMsg *message.Message, toolMsg **message.Message, failure *error) {

	if *failure == nil {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if *toolMsg == nil && len(assistantMsg.ToolCalls()) > 0 {
		parts := make([]message.ContentPart, 0, len(assistantMsg.ToolCalls()))
		for _, call := range assistantMsg.ToolCalls() {
			parts = append(parts, message.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: "Turn interrupted. Inspect current files/processes before retrying; do not assume an operation was rolled back.", IsError: true})
		}
		saved, err := a.messages.Create(saveCtx, sessionID, message.CreateMessageParams{Role: message.Tool, Parts: parts})
		if err != nil {
			*failure = errors.Join(*failure, err)
		} else {
			*toolMsg = &saved
		}
	}
	reason := message.FinishReasonError
	if errors.Is(*failure, context.Canceled) {
		reason = message.FinishReasonCanceled
	}
	if len(assistantMsg.ToolCalls()) > 0 {
		assistantMsg.SetRevisionOf("")
	}
	if assistantMsg.CitationValidation() == message.CitationPending {
		assistantMsg.SetCitationValidation(message.CitationInvalid)
	}
	a.finishMessage(saveCtx, assistantMsg, reason)
}
func (a *agent) toolExecutionContext(ctx context.Context, sessionID, messageID string) context.Context {
	// Add the session and message ID into the context if needed by tools.
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, messageID)
	ctx = tools.WithWorkflowStore(ctx, a.workflow)
	if a.workflow != nil {
		ctx = tools.WithMutationObserver(ctx, func(changes []tools.FileChange) error {
			saveCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			return a.recordFileChanges(saveCtx, sessionID, changes)
		})
	}

	return ctx
}
func (a *agent) readAssistantEvents(ctx context.Context, sessionID string, assistantMsg *message.Message, eventChan <-chan provider.ProviderEvent, providerStarted time.Time, streamUsage *streamUsage) error {
	reader := assistantStreamReader{agent: a, sessionID: sessionID, assistant: assistantMsg, started: providerStarted, usage: streamUsage, writer: newStreamMessageWriter(a.messages, assistantMsg, time.Now())}
	// The interruption guard persists this materialized content even if ctx was
	// canceled or the last checkpoint failed. No timer goroutine owns the message.
	defer reader.writer.materialize()
	ticker := time.NewTicker(streamPublishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			if err := reader.writer.flush(ctx, now, false); err != nil {
				return err
			}
		case event, ok := <-eventChan:
			if !ok {
				return reader.ended(ctx)
			}
			if err := reader.consume(ctx, event); err != nil {
				return err
			}
		}
	}
}

type assistantStreamReader struct {
	agent                     *agent
	sessionID                 string
	assistant                 *message.Message
	started                   time.Time
	usage                     *streamUsage
	writer                    *streamMessageWriter
	completed, firstGenerated bool
}

func (r *assistantStreamReader) consume(ctx context.Context, event provider.ProviderEvent) error {
	r.agent.observeStreamUsage(r.usage, event)
	if !r.firstGenerated && generatedEvent(event) {
		r.agent.observePhase(r.sessionID, "first_token", time.Since(r.started))
		r.firstGenerated = true
	}
	if event.Type == provider.EventComplete {
		r.completed = true
		event = observedProviderDuration(event, r.started)
		r.writer.materialize()
		// Completion replaces tool parts and can move text indexes. All buffered
		// text is already materialized; discard its old part bindings first.
		r.writer.buffer = message.NewStreamBuffer(r.assistant)
	} else if err := r.writer.handle(ctx, event); err != nil {
		return err
	}
	if event.Type == provider.EventContentDelta || event.Type == provider.EventThinkingDelta {
		return nil
	}
	if err := r.agent.processEvent(ctx, r.sessionID, r.assistant, event); err != nil {
		return err
	}
	if event.Type == provider.EventComplete {
		// Completion's durable update already contains the buffered text.
		r.writer.unpersisted, r.writer.unpublished = 0, 0
	}
	return nil
}

func (r *assistantStreamReader) ended(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !r.completed {
		return fmt.Errorf("provider stream ended without a completion event")
	}
	return nil
}

type toolBatchExecution struct {
	agent     *agent
	ctx       context.Context
	sessionID string
	run       *generationRun
	assistant *message.Message
	calls     []message.ToolCall
	results   []message.ToolResult
	reads     map[string]bool
}

func (a *agent) executeToolBatch(ctx context.Context, sessionID string, assistant *message.Message, run *generationRun) (*message.Message, error) {
	calls := assistant.ToolCalls()
	b := toolBatchExecution{agent: a, ctx: ctx, sessionID: sessionID, assistant: assistant, run: run, calls: calls, results: make([]message.ToolResult, len(calls)), reads: batchSourceReads(calls)}
	for i, call := range calls {
		if reason := b.stopReason(); reason != "" {
			b.skip(i, reason)
			break
		}
		result, denied, err := b.execute(call)
		if err != nil {
			return nil, err
		}
		b.results[i] = result
		if denied {
			b.skip(i+1, "Tool execution canceled by user")
			a.finishMessage(ctx, assistant, message.FinishReasonPermissionDenied)
			break
		}
	}
	return b.save()
}
func (b *toolBatchExecution) stopReason() string {
	if len(b.calls) > 16 || b.run.toolBytes >= b.run.limits.ToolBudget {
		return "Tool call budget exceeded; no operation executed"
	}
	if b.agent.hasInstructions(b.sessionID) {
		return "Tool skipped because the user sent new instructions"
	}
	if b.ctx.Err() != nil {
		b.agent.finishMessage(context.Background(), b.assistant, message.FinishReasonCanceled)
		return "Tool execution canceled by user"
	}
	return ""
}
func (b *toolBatchExecution) skip(start int, reason string) {
	for i := start; i < len(b.calls); i++ {
		b.results[i] = message.ToolResult{ToolCallID: b.calls[i].ID, Content: reason, IsError: true}
	}
}
func (b *toolBatchExecution) findTool(call message.ToolCall) tools.BaseTool {
	for _, tool := range b.run.available {
		if tool.Info().Name != call.Name || !runAllowsTool(b.run, tool) || !b.run.selected[call.Name] {
			continue
		}
		if !coreSchema(call.Name) {
			if b.run.toolLeases == nil {
				b.run.toolLeases = map[string]int{}
			}
			b.run.toolLeases[call.Name] = b.run.steps + 8
		}
		return tool
	}
	return nil
}
func (b *toolBatchExecution) execute(call message.ToolCall) (message.ToolResult, bool, error) {
	tool := b.findTool(call)
	if tool == nil {
		return toolBatchError(call, fmt.Sprintf("Tool unavailable in %s mode or not loaded: %s. Use get_tools_catalog/request_tools.", b.run.mode, call.Name)), false, nil
	}
	if mutationAwaitingSource(call, b.reads) {
		return toolBatchError(call, "No change applied: this response also requests the file's source. Read the actual tool result first, then call the edit/write/patch tool in the next response using exact source text. Do not repeat the read and edit together."), false, nil
	}
	before, err := b.prepareChecks(call)
	if err != nil {
		return message.ToolResult{}, false, err
	}
	result, toolErr, fatal := b.invoke(tool, call)
	if fatal != nil {
		return message.ToolResult{}, false, fatal
	}
	if errors.Is(toolErr, permission.ErrorPermissionDenied) {
		return toolBatchError(call, "Permission denied"), true, nil
	}
	if toolErr != nil {
		result = tools.NewTextErrorResponse(toolErr.Error())
	}
	b.recordChecks(call, before, toolErr, &result)
	return message.ToolResult{ToolCallID: call.ID, Content: result.Content, Metadata: result.Metadata, IsError: result.IsError}, false, nil
}
func toolBatchError(call message.ToolCall, text string) message.ToolResult {
	return message.ToolResult{ToolCallID: call.ID, Content: text, IsError: true}
}
func (b *toolBatchExecution) prepareChecks(call message.ToolCall) (string, error) {
	if b.agent.workflow == nil || call.Name != "run_checks" {
		return "", nil
	}
	if err := b.agent.observeWorkspace(b.ctx, b.sessionID); err != nil {
		logging.Warn("Check fingerprint unavailable", "error", err)
	}
	task, err := b.agent.workflow.Get(b.ctx, b.sessionID)
	if err != nil {
		return "", err
	}
	var input struct{ Names []string }
	_ = json.Unmarshal([]byte(call.Input), &input)
	names := input.Names
	if len(names) == 0 {
		for name := range checkCommands() {
			names = append(names, name)
		}
	}
	if err := b.agent.workflow.InvalidateChecks(b.ctx, b.sessionID, names); err != nil {
		return "", err
	}
	return task.Digest, nil
}
func (b *toolBatchExecution) invoke(tool tools.BaseTool, call message.ToolCall) (tools.ToolResponse, error, error) {
	if b.agent.workflow != nil && unscopedTool(tool) {
		if err := b.agent.workflow.RequireWorkspace(b.ctx, b.sessionID); err != nil {
			return tools.ToolResponse{}, nil, err
		}
	}
	if err := b.agent.runHooks(b.ctx, b.run, "before_tool", call.Name); err != nil {
		return tools.ToolResponse{}, nil, err
	}
	result, toolErr := b.agent.runTimedTool(b.sessionID, func() (tools.ToolResponse, error) {
		return tool.Run(b.agent.attachExplorationBudget(b.ctx, b.run), tools.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input})
	})
	if toolErr == nil {
		if err := b.agent.runHooks(b.ctx, b.run, "after_tool", call.Name); err != nil {
			return tools.ToolResponse{}, nil, err
		}
	}
	return result, toolErr, nil
}
func (b *toolBatchExecution) recordChecks(call message.ToolCall, before string, toolErr error, result *tools.ToolResponse) {
	if b.agent.workflow == nil || toolErr != nil || call.Name != "run_checks" {
		return
	}
	if err := b.agent.recordChecks(b.ctx, b.sessionID, before, *result); err != nil {
		result.IsError = true
		result.Content += "\nValidation could not be recorded: " + err.Error()
	}
}
func (b *toolBatchExecution) save() (*message.Message, error) {
	for i := range b.results {
		remaining := max(0, b.run.limits.ToolBudget-b.run.toolBytes)
		b.results[i].Content = clip(b.results[i].Content, min(16000, remaining))
		b.run.toolBytes += len(b.results[i].Content)
		if len(b.results[i].Metadata) > min(4096, max(0, b.run.limits.ToolBudget-b.run.toolBytes)) {
			b.results[i].Metadata = ""
		}
		b.run.toolBytes += len(b.results[i].Metadata)
	}
	if len(b.results) == 0 {
		return nil, nil
	}
	parts := make([]message.ContentPart, 0, len(b.results))
	for _, result := range b.results {
		parts = append(parts, result)
	}
	saved, err := b.agent.messages.Create(context.Background(), b.assistant.SessionID, message.CreateMessageParams{Role: message.Tool, Parts: parts})
	if err != nil {
		return nil, fmt.Errorf("failed to create cancelled tool message: %w", err)
	}
	return &saved, nil
}

func observedProviderDuration(event provider.ProviderEvent, started time.Time) provider.ProviderEvent {
	if event.Response == nil || event.Response.Usage.OutputDurationNS > 0 {
		return event
	}
	copy := *event.Response
	copy.Usage.OutputDurationNS = time.Since(started).Nanoseconds()
	copy.Usage.DurationSource = "observed"
	event.Response = &copy
	return event
}
