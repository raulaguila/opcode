package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/prompt"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/workflow"
)

// Common errors
var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
)

type AgentEventType string

const (
	AgentEventTypeError        AgentEventType = "error"
	AgentEventTypeResponse     AgentEventType = "response"
	AgentEventTypeSummarize    AgentEventType = "summarize"
	AgentEventTypeInstructions AgentEventType = "instructions"
)

type AgentEvent struct {
	Type    AgentEventType
	Message message.Message
	Error   error

	// When summarizing
	SessionID string
	Progress  string
	Done      bool
	Outcome   string // completed, planned, blocked or partial; never permission.
}

type Service interface {
	pubsub.Suscriber[AgentEvent]
	Model() models.Model
	SetMode(context.Context, string, workflow.Mode) error
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	Submit(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, bool, error)
	Cancel(sessionID string)
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error)
	SelectModel(agentName config.AgentName, model models.Model) (models.Model, error)
	Summarize(ctx context.Context, sessionID string) error
}

type agent struct {
	*pubsub.Broker[AgentEvent]
	sessions session.Service
	messages message.Service

	tools     []tools.BaseTool
	provider  provider.Provider
	agentName config.AgentName

	titleProvider     provider.Provider
	summarizeProvider provider.Provider

	activeRequests    sync.Map
	runMu             sync.Mutex
	runs              map[string]*generationRun
	closed            bool
	workflow          *workflow.Store
	questions         *interaction.Questions
	onModeChange      func(string)
	systemMessage     string
	maxOutputTokens   int64
	contextMu         sync.Mutex
	contextStatuses   map[string]ContextStatus
	compactSessions   map[string]bool
	tokenObservations map[string]tokenObservation
	usageMetrics      map[string]UsageMetrics
	streamUsages      map[string]*streamUsage
	performance       map[string]*performanceState
}

func NewAgent(
	agentName config.AgentName,
	sessions session.Service,
	messages message.Service,
	agentTools []tools.BaseTool,
	options ...Option,
) (Service, error) {
	agentProvider, err := createAgentProvider(agentName)
	if err != nil {
		return nil, err
	}
	var titleProvider provider.Provider
	// Only generate titles for the coder agent
	if agentName == config.AgentCoder {
		titleProvider, err = createAgentProvider(config.AgentTitle)
		if err != nil {
			return nil, err
		}
	}
	var summarizeProvider provider.Provider
	if agentName == config.AgentCoder {
		summarizeProvider, err = createAgentProvider(config.AgentSummarizer)
		if err != nil {
			return nil, err
		}
	}

	agent := &agent{
		Broker:            pubsub.NewBroker[AgentEvent](),
		provider:          agentProvider,
		agentName:         agentName,
		messages:          messages,
		sessions:          sessions,
		tools:             agentTools,
		titleProvider:     titleProvider,
		summarizeProvider: summarizeProvider,
		activeRequests:    sync.Map{},
	}

	agent.systemMessage, agent.maxOutputTokens = configuredPrompt(agentName, agentProvider.Model())
	for _, option := range options {
		option(agent)
	}
	return agent, nil
}

func (a *agent) Model() models.Model {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	return a.provider.Model()
}

func (a *agent) Cancel(sessionID string) {
	for _, tool := range a.tools {
		if closer, ok := tool.(interface{ CancelSession(string) }); ok {
			closer.CancelSession(sessionID)
		}
	}
	// Cancel regular requests
	a.runMu.Lock()
	if run := a.runs[sessionID]; run != nil {
		run.accepting = false
		run.pending = nil
		run.cancel()
	}
	a.runMu.Unlock()

	// Also check for summarize requests
	if cancelFunc, exists := a.activeRequests.Load(sessionID + "-summarize"); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Summarize cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}
}

func (a *agent) IsBusy() bool {
	busy := false
	a.activeRequests.Range(func(key, value interface{}) bool {
		if _, ok := value.(*generationRun); ok {
			busy = true
			return false
		}
		if cancelFunc, ok := value.(context.CancelFunc); ok {
			if cancelFunc != nil {
				busy = true
				return false // Stop iterating
			}
		}
		return true // Continue iterating
	})
	return busy
}

func (a *agent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Load(sessionID)
	_, summarizing := a.activeRequests.Load(sessionID + "-summarize")
	return busy || summarizing
}

func (a *agent) generateTitle(ctx context.Context, sessionID string, content string) error {
	if content == "" {
		return nil
	}
	a.runMu.Lock()
	titleProvider := a.titleProvider
	mainModel := models.Model{}
	if a.provider != nil {
		mainModel = a.provider.Model()
	}
	a.runMu.Unlock()
	if titleProvider == nil {
		return nil
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	// Avoid queueing a second inference on the same local model merely to name
	// the conversation. A separately selected title model keeps its old behavior.
	if mainModel.Provider == models.ProviderLocal && mainModel.ID != "" && titleProvider.Model().ID == mainModel.ID {
		session.Title = localConversationTitle(content)
		if session.Title == "" {
			return nil
		}
		_, err := a.sessions.Save(ctx, session)
		if err == nil {
			a.observeLocalTitle(sessionID)
		}
		return err
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	parts := []message.ContentPart{message.TextContent{Text: content}}
	titleHistory, err := auxiliaryBudget(config.AgentTitle, titleProvider, []message.Message{{Role: message.User, Parts: parts}})
	if err != nil {
		return err
	}
	response, err := timedProvider(titleProvider).SendMessages(ctx, titleHistory, nil)
	if err != nil {
		return err
	}

	title := strings.TrimSpace(strings.ReplaceAll(response.Content, "\n", " "))
	if title == "" {
		return nil
	}

	session.Title = title
	_, err = a.sessions.Save(ctx, session)
	return err
}

func (a *agent) err(err error) AgentEvent {
	return AgentEvent{
		Type:  AgentEventTypeError,
		Error: err,
	}
}

func (a *agent) processGeneration(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart, run *generationRun) AgentEvent {
	stopPreparation := a.startPhase(sessionID, "context")
	defer stopPreparation()
	// List existing messages; if none, start title generation asynchronously.
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to list messages: %w", err))
	}
	if len(msgs) == 0 {
		go func() {
			defer logging.RecoverPanic("agent.Run", func() {
				logging.ErrorPersist("panic while generating title")
			})
			titleErr := a.generateTitle(ctx, sessionID, content)
			if titleErr != nil {
				logging.ErrorPersist(fmt.Sprintf("failed to generate title: %v", titleErr))
			}
		}()
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to get session: %w", err))
	}
	if session.SummaryMessageID != "" {
		summaryMsgInex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgInex = i
				break
			}
		}
		if summaryMsgInex != -1 {
			msgs = msgs[summaryMsgInex:]
			msgs[0].Role = message.User
		}
	}

	userMsg, err := a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return a.err(fmt.Errorf("failed to create user message: %w", err))
	}
	run.startMessageID = userMsg.ID
	run.turnMessageID = userMsg.ID
	if resumingRequest(content) {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == message.User && !msgs[i].IsApplication() && !strings.HasPrefix(msgs[i].Content().Text, "Application validation:") && !resumingRequest(msgs[i].Content().Text) {
				requiredToolCalls(msgs[i].Content().Text, run)
				run.startMessageID = msgs[i].ID
				break
			}
		}
	}
	// Append the new user message to the conversation history.
	msgHistory := append(msgs, userMsg)
	a.prefetchTools(run, content)
	msgHistory, err = a.bootstrap(ctx, sessionID, content, msgHistory, run)
	if err != nil {
		return a.err(err)
	}
	if a.workflow != nil {
		if err := a.observeWorkspace(ctx, sessionID); err != nil {
			logging.Warn("Workspace validation unavailable", "error", err)
		}
	}
	if a.workflow != nil && run.mode == workflow.Execute {
		task, err := a.workflow.Get(ctx, sessionID)
		if err != nil {
			return a.err(err)
		}
		requires := requiresFileChanges(content) || (resumingRequest(content) && requiresFileChanges(task.Objective))
		if run.contract != nil {
			requires = run.contract.Kind == "change"
		}
		if _, err := a.workflow.BeginTurn(ctx, sessionID, requires, resumingRequest(content)); err != nil {
			return a.err(err)
		}
	}
	signatures := make([][32]byte, 0, 10)
	repairs := 0
	citationRepairs := 0
	contextRetries := 0
	finalAnswerRetries := 0
	stopPreparation()
	for {
		if run.steps >= run.limits.MaxSteps {
			return a.err(fmt.Errorf("agent step limit reached (%d); progress saved", run.limits.MaxSteps))
		}
		if run.toolBytes >= run.limits.ToolBudget {
			return a.err(fmt.Errorf("tool result budget reached (%d bytes); progress saved", run.limits.ToolBudget))
		}
		run.steps++
		// Check for cancellation before each iteration
		select {
		case <-ctx.Done():
			return a.err(ctx.Err())
		default:
			// Continue processing
		}
		agentMessage, toolResults, err := a.streamAndHandleEvents(ctx, sessionID, msgHistory, run)
		if err != nil {
			if finalAnswerRetries < 1 && errors.Is(err, provider.ErrInvalidFinalAnswer) && ctx.Err() == nil && len(agentMessage.ToolCalls()) == 0 && agentMessage.Content().String() == "" && agentMessage.ReasoningContent().Thinking == "" {
				finalAnswerRetries++
				a.observePhase(sessionID, "repair", 0)
				correction, e := a.createApplicationMessage(ctx, sessionID, "Application protocol correction: the final reply had invalid JSON fields. Return a valid reply using the advertised final response schema and answer the original request in its language. Keep all completed actions and real tool results; do not repeat edits or commands. This correction grants no permissions.", nil)
				if e != nil {
					return a.err(e)
				}
				msgHistory = append(msgHistory, correction)
				continue
			}
			// Retry generation only: no tools execute before a complete response.
			if contextRetries < 2 && len(agentMessage.ToolCalls()) == 0 && agentMessage.Content().String() == "" && agentMessage.ReasoningContent().Thinking == "" && a.adaptContext(run, err) {
				contextRetries++
				a.observePhase(sessionID, "repair", 0)
				a.Publish(pubsub.UpdatedEvent, AgentEvent{Type: AgentEventTypeInstructions, SessionID: sessionID, Progress: "Context limit reduced after provider memory/context failure; retrying generation only"})
				continue
			}
			if errors.Is(err, context.Canceled) {
				agentMessage.AddFinish(message.FinishReasonCanceled)
				a.messages.Update(context.Background(), agentMessage)
				return a.err(ErrRequestCancelled)
			}
			return a.err(fmt.Errorf("failed to process events: %w", err))
		}
		resultCount := 0
		if toolResults != nil {
			resultCount = len(toolResults.ToolResults())
		}
		logging.Info("Agent operation completed", "finish_reason", agentMessage.FinishReason(), "tool_results", resultCount)

		msgHistory = append(msgHistory, agentMessage)
		if toolResults != nil {
			msgHistory = append(msgHistory, *toolResults)
		}
		if toolResults != nil && len(agentMessage.ToolCalls()) > 0 && !a.hasInstructions(sessionID) {
			signature := toolSignature(agentMessage.ToolCalls(), toolResults)
			signatures = append(signatures, signature)
			if len(signatures) > 10 {
				signatures = signatures[1:]
			}
			repeats := 0
			for _, value := range signatures {
				if value == signature {
					repeats++
				}
			}
			if repeats >= 4 {
				return a.err(fmt.Errorf("repeated tool calls without progress; task saved for resumption"))
			}
		}
		continueTools := agentMessage.FinishReason() == message.FinishReasonToolUse && toolResults != nil
		instructions := a.takeInstructions(run, !continueTools)
		for _, instruction := range instructions {
			msg, err := a.createUserMessage(ctx, sessionID, instruction.content, instruction.parts)
			if err != nil {
				return a.err(fmt.Errorf("failed to save new instruction: %w", err))
			}
			msgHistory = append(msgHistory, msg)
			a.prefetchTools(run, instruction.content)
			if a.workflow != nil && run.mode == workflow.Execute && run.contract == nil && requiresFileChanges(instruction.content) {
				if _, err := a.workflow.BeginTurn(ctx, sessionID, true); err != nil {
					return a.err(err)
				}
			}
		}
		if len(instructions) > 0 {
			signatures = signatures[:0]
			repairs = 0
			a.Publish(pubsub.UpdatedEvent, AgentEvent{Type: AgentEventTypeInstructions, SessionID: sessionID, Progress: "New instructions applied"})
		}
		if continueTools || len(instructions) > 0 {
			continue
		}
		if a.workflow != nil && run.mode != workflow.Ask {
			fingerprintErr := a.observeWorkspace(ctx, sessionID)
			task, err := a.workflow.Get(ctx, sessionID)
			if err != nil {
				return a.err(err)
			}
			if fingerprintErr != nil && run.mode == workflow.Execute && (task.Changed || len(task.Checks) > 0 || task.Status == "completed") {
				return a.err(fmt.Errorf("cannot verify current files: %w", fingerprintErr))
			}
			// Changes with no configured checks are observable partial work, not an
			// impossible instruction to the model to execute an unknown command.
			if run.mode == workflow.Execute && task.Changed && len(task.Checks) == 0 && len(checkCommands()) == 0 && (run.contract == nil || len(run.contract.Checks) == 0) && task.Objective == "" && task.Status != "partial" && task.Status != "blocked" {
				if _, err := a.workflow.FinishWithReason(ctx, sessionID, "partial", "File changes applied; no configured checks are available to verify this revision", "", nil); err != nil {
					return a.err(err)
				}
				task, err = a.workflow.Get(ctx, sessionID)
				if err != nil {
					return a.err(err)
				}
			}
			problem := task.CompletionProblem()
			if run.mode == workflow.Plan && run.requiresPlan && (strings.TrimSpace(task.Objective) == "" || len(task.Steps) == 0) {
				problem = "A structured saved plan was explicitly requested, but no valid objective and steps were saved. Call update_plan using its advertised argument schema to save objective, 1–24 steps and at least one criterion, then finish_task as planned. If unable to save the plan, mark the task blocked or partial instead of claiming success."
			}
			if task.Objective != "" && task.Status == "in_progress" {
				problem = "An active plan remains unfinished. Update the steps and call finish_task (planned in plan mode, completed or blocked in execute mode)."
			}
			if run.mode == workflow.Execute && (task.Changed || (run.contract != nil && len(run.contract.Checks) > 0)) {
				if why := task.ValidationProblem(checkCommands()); why != "" {
					problem = why
				}
			}
			if problem != "" && task.Status != "blocked" && task.Status != "partial" {
				if repairs >= 2 {
					return a.err(fmt.Errorf("task cannot be concluded: %s", problem))
				}
				repairs++
				a.observePhase(sessionID, "repair", 0)
				correction, err := a.createApplicationMessage(ctx, sessionID, "Application validation: "+problem, nil)
				if err != nil {
					return a.err(err)
				}
				msgHistory = append(msgHistory, correction)
				run.selected["run_checks"] = true
				a.runMu.Lock()
				if ctx.Err() == nil {
					run.accepting = true
				}
				a.runMu.Unlock()
				continue
			}
		}
		if missing := missingRequiredCalls(run, msgHistory); len(missing) > 0 {
			taskPartial := false
			if a.workflow != nil {
				if task, e := a.workflow.Get(ctx, sessionID); e == nil {
					taskPartial = task.Status == "partial" || task.Status == "blocked"
				}
			}
			if !taskPartial {
				if repairs >= 2 {
					return a.err(fmt.Errorf("required actions remain unverified: %s; progress preserved", strings.Join(missing, ", ")))
				}
				repairs++
				a.observePhase(sessionID, "repair", 0)
				correction, e := a.createApplicationMessage(ctx, sessionID, "Application validation: explicitly requested actions lack successful receipts: "+strings.Join(missing, ", ")+". Perform these authorized steps before concluding; use partial/blocked if unable. Do not repeat completed side effects.", nil)
				if e != nil {
					return a.err(e)
				}
				msgHistory = append(msgHistory, correction)
				for name := range run.requiredCalls {
					run.toolLeases[name] = run.steps + 8
					run.selected[name] = true
				}
				a.runMu.Lock()
				if ctx.Err() == nil {
					run.accepting = true
				}
				a.runMu.Unlock()
				continue
			}
		}
		if problem := missingSourceCitation(run, msgHistory, agentMessage.Content().Text); problem != "" {
			if citationRepairs >= 1 {
				return a.err(fmt.Errorf("source citation remains missing after repair; read evidence preserved: %s", clip(tools.RedactCredentials(problem), 1800)))
			}
			citationRepairs++
			a.observePhase(sessionID, "repair", 0)
			run.citationRepair = true
			correction, err := a.createApplicationMessage(ctx, sessionID, citationRepairRequest(run, msgHistory, problem), nil)
			if err != nil {
				return a.err(err)
			}
			msgHistory = append(msgHistory, correction)
			a.runMu.Lock()
			if ctx.Err() == nil {
				run.accepting = true
			}
			a.runMu.Unlock()
			continue
		}
		outcome := "completed"
		if a.workflow != nil {
			if task, err := a.workflow.Get(ctx, sessionID); err == nil && (task.Status == "blocked" || task.Status == "partial" || task.Status == "planned") {
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
}

func (a *agent) createUserMessage(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) (message.Message, error) {
	parts := []message.ContentPart{message.TextContent{Text: content}}
	parts = append(parts, attachmentParts...)
	return a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
}

func (a *agent) createApplicationMessage(ctx context.Context, sessionID, content string, _ []message.ContentPart) (message.Message, error) {
	msg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: content, Origin: "application"}},
	})
	if err == nil {
		a.Publish(pubsub.UpdatedEvent, AgentEvent{Type: AgentEventTypeResponse, SessionID: sessionID, Message: msg})
	}
	return msg, err
}

func (a *agent) streamAndHandleEvents(ctx context.Context, sessionID string, msgHistory []message.Message, run *generationRun) (assistantMsg message.Message, toolMsg *message.Message, failure error) {
	stopContext := a.startPhase(sessionID, "context")
	defer stopContext()
	ctx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	requestHistory, requestErr := a.requestHistory(ctx, sessionID, msgHistory, run)
	if requestErr != nil {
		return message.Message{}, nil, requestErr
	}
	if controller, ok := runProvider(a, run).(provider.SystemPromptController); ok {
		names := make([]string, 0)
		for _, tool := range a.exposedTools(run) {
			names = append(names, tool.Info().Name)
		}
		model := runProvider(a, run).Model()
		next, err := controller.WithSystemPrompt(prompt.RunPrompt(string(run.mode), model.API, model.ContextWindow, names))
		if err != nil {
			return message.Message{}, nil, err
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
			return message.Message{}, nil, err
		}
		run.provider = next
	}
	requestHistory, requestErr = a.budgetRequest(sessionID, requestHistory, run)
	if requestErr != nil {
		return message.Message{}, nil, requestErr
	}
	stopContext()
	providerStarted := time.Now()
	stopProvider := a.startPhase(sessionID, "provider")
	defer stopProvider()
	streamModel := runProvider(a, run).Model()
	streamUsage := a.startStreamUsage(sessionID, streamModel.ID, streamModel)
	defer a.finishStreamUsage(sessionID, streamUsage)
	eventChan := timedProvider(runProvider(a, run)).StreamResponse(ctx, requestHistory, a.exposedTools(run))

	var err error
	assistantMsg, err = a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{},
		Model: a.provider.Model().ID,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	defer func() {
		if failure == nil {
			return
		}
		saveCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if toolMsg == nil && len(assistantMsg.ToolCalls()) > 0 {
			parts := make([]message.ContentPart, 0, len(assistantMsg.ToolCalls()))
			for _, call := range assistantMsg.ToolCalls() {
				parts = append(parts, message.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: "Turn interrupted. Inspect current files/processes before retrying; do not assume an operation was rolled back.", IsError: true})
			}
			saved, err := a.messages.Create(saveCtx, sessionID, message.CreateMessageParams{Role: message.Tool, Parts: parts})
			if err != nil {
				failure = errors.Join(failure, err)
			} else {
				toolMsg = &saved
			}
		}
		reason := message.FinishReasonError
		if errors.Is(failure, context.Canceled) {
			reason = message.FinishReasonCanceled
		}
		a.finishMessage(saveCtx, &assistantMsg, reason)
	}()
	// Add the session and message ID into the context if needed by tools.
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, assistantMsg.ID)
	ctx = tools.WithWorkflowStore(ctx, a.workflow)
	if a.workflow != nil {
		ctx = tools.WithMutationObserver(ctx, func() error {
			saveCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			return a.workflow.Mutated(saveCtx, sessionID)
		})
	}

	completed := false
	firstGenerated := false
	// Process each event in the stream.
	for event := range eventChan {
		a.observeStreamUsage(streamUsage, event)
		if !firstGenerated && generatedEvent(event) {
			a.observePhase(sessionID, "first_token", time.Since(providerStarted))
			firstGenerated = true
		}
		if event.Type == provider.EventComplete {
			completed = true
			if event.Response != nil && event.Response.Usage.OutputDurationNS <= 0 {
				copy := *event.Response
				copy.Usage.OutputDurationNS = time.Since(providerStarted).Nanoseconds()
				copy.Usage.DurationSource = "observed"
				event.Response = &copy
			}
		}
		if processErr := a.processEvent(ctx, sessionID, &assistantMsg, event); processErr != nil {
			a.finishMessage(ctx, &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, processErr
		}
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, ctx.Err()
		}
	}

	stopProvider()
	a.finishStreamUsage(sessionID, streamUsage) // Tools and approvals are not generation.
	if ctx.Err() != nil {
		a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
		return assistantMsg, nil, ctx.Err()
	}
	if !completed {
		a.finishMessage(ctx, &assistantMsg, message.FinishReasonError)
		return assistantMsg, nil, fmt.Errorf("provider stream ended without a completion event")
	}
	toolResults := make([]message.ToolResult, len(assistantMsg.ToolCalls()))
	toolCalls := assistantMsg.ToolCalls()
	batchReads := batchSourceReads(toolCalls)
	for i, toolCall := range toolCalls {
		if len(toolCalls) > 16 || run.toolBytes >= run.limits.ToolBudget {
			for j := i; j < len(toolCalls); j++ {
				toolResults[j] = message.ToolResult{ToolCallID: toolCalls[j].ID, Content: "Tool call budget exceeded; no operation executed", IsError: true}
			}
			goto out
		}
		if a.hasInstructions(sessionID) {
			for j := i; j < len(toolCalls); j++ {
				toolResults[j] = message.ToolResult{ToolCallID: toolCalls[j].ID, Content: "Tool skipped because the user sent new instructions", IsError: true}
			}
			goto out
		}
		select {
		case <-ctx.Done():
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			// Make all future tool calls cancelled
			for j := i; j < len(toolCalls); j++ {
				toolResults[j] = message.ToolResult{
					ToolCallID: toolCalls[j].ID,
					Content:    "Tool execution canceled by user",
					IsError:    true,
				}
			}
			goto out
		default:
			// Continue processing
			var tool tools.BaseTool
			for _, availableTool := range run.available {
				if availableTool.Info().Name == toolCall.Name && runAllowsTool(run, availableTool) && run.selected[toolCall.Name] {
					tool = availableTool
					break
				}
				// Monkey patch for Copilot Sonnet-4 tool repetition obfuscation
				// if strings.HasPrefix(toolCall.Name, availableTool.Info().Name) &&
				// 	strings.HasPrefix(toolCall.Name, availableTool.Info().Name+availableTool.Info().Name) {
				// 	tool = availableTool
				// 	break
				// }
			}

			if tool != nil && !coreSchema(toolCall.Name) {
				if run.toolLeases == nil {
					run.toolLeases = map[string]int{}
				}
				run.toolLeases[toolCall.Name] = run.steps + 8
			}
			// Tool not found
			if tool == nil {
				toolResults[i] = message.ToolResult{
					ToolCallID: toolCall.ID,
					Content:    fmt.Sprintf("Tool unavailable in %s mode or not loaded: %s. Use get_tools_catalog/request_tools.", run.mode, toolCall.Name),
					IsError:    true,
				}
				continue
			}
			if mutationAwaitingSource(toolCall, batchReads) {
				toolResults[i] = message.ToolResult{ToolCallID: toolCall.ID, Content: "No change applied: this response also requests the file's source. Read the actual tool result first, then call the edit/write/patch tool in the next response using exact source text. Do not repeat the read and edit together.", IsError: true}
				continue
			}
			before := ""
			if a.workflow != nil && toolCall.Name == "run_checks" {
				if err := a.observeWorkspace(ctx, sessionID); err != nil {
					logging.Warn("Check fingerprint unavailable", "error", err)
				}
				task, err := a.workflow.Get(ctx, sessionID)
				if err != nil {
					return assistantMsg, nil, err
				}
				before = task.Digest
				var input struct{ Names []string }
				_ = json.Unmarshal([]byte(toolCall.Input), &input)
				names := input.Names
				if len(names) == 0 {
					for name := range checkCommands() {
						names = append(names, name)
					}
				}
				if err := a.workflow.InvalidateChecks(ctx, sessionID, names); err != nil {
					return assistantMsg, nil, err
				}
			}
			if hookErr := a.runHooks(ctx, run, "before_tool", toolCall.Name); hookErr != nil {
				return assistantMsg, nil, hookErr
			}
			toolResult, toolErr := a.runTimedTool(sessionID, func() (tools.ToolResponse, error) {
				return tool.Run(a.attachExplorationBudget(ctx, run), tools.ToolCall{
					ID:    toolCall.ID,
					Name:  toolCall.Name,
					Input: toolCall.Input,
				})
			})
			if toolErr == nil {
				if hookErr := a.runHooks(ctx, run, "after_tool", toolCall.Name); hookErr != nil {
					return assistantMsg, nil, hookErr
				}
			}
			if toolErr != nil {
				if errors.Is(toolErr, permission.ErrorPermissionDenied) {
					toolResults[i] = message.ToolResult{
						ToolCallID: toolCall.ID,
						Content:    "Permission denied",
						IsError:    true,
					}
					for j := i + 1; j < len(toolCalls); j++ {
						toolResults[j] = message.ToolResult{
							ToolCallID: toolCalls[j].ID,
							Content:    "Tool execution canceled by user",
							IsError:    true,
						}
					}
					a.finishMessage(ctx, &assistantMsg, message.FinishReasonPermissionDenied)
					break
				}
				toolResult = tools.NewTextErrorResponse(toolErr.Error())
			}
			if a.workflow != nil && toolErr == nil && toolCall.Name == "run_checks" {
				if err := a.recordChecks(ctx, sessionID, before, toolResult); err != nil {
					toolResult.IsError = true
					toolResult.Content += "\nValidation could not be recorded: " + err.Error()
				}
			}
			toolResults[i] = message.ToolResult{
				ToolCallID: toolCall.ID,
				Content:    toolResult.Content,
				Metadata:   toolResult.Metadata,
				IsError:    toolResult.IsError,
			}
		}
	}
out:
	for i := range toolResults {
		remaining := max(0, run.limits.ToolBudget-run.toolBytes)
		toolResults[i].Content = clip(toolResults[i].Content, min(16000, remaining))
		run.toolBytes += len(toolResults[i].Content)
		if len(toolResults[i].Metadata) > min(4096, max(0, run.limits.ToolBudget-run.toolBytes)) {
			toolResults[i].Metadata = ""
		}
		run.toolBytes += len(toolResults[i].Metadata)
	}
	if len(toolResults) == 0 {
		return assistantMsg, nil, nil
	}
	parts := make([]message.ContentPart, 0)
	for _, tr := range toolResults {
		parts = append(parts, tr)
	}
	msg, err := a.messages.Create(context.Background(), assistantMsg.SessionID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: parts,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create cancelled tool message: %w", err)
	}

	return assistantMsg, &msg, err
}

func (a *agent) finishMessage(ctx context.Context, msg *message.Message, finishReson message.FinishReason) {
	msg.AddFinish(finishReson)
	_ = a.messages.Update(ctx, *msg)
}

func (a *agent) processEvent(ctx context.Context, sessionID string, assistantMsg *message.Message, event provider.ProviderEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case provider.EventThinkingDelta:
		assistantMsg.AppendReasoningContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventContentDelta:
		assistantMsg.AppendContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventToolUseStart:
		call := *event.ToolCall
		call.Origin = "" // Provider payloads cannot assign application-owned provenance.
		assistantMsg.AddToolCall(call)
		return a.messages.Update(ctx, *assistantMsg)
	// TODO: see how to handle this
	// case provider.EventToolUseDelta:
	// 	tm := time.Unix(assistantMsg.UpdatedAt, 0)
	// 	assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)
	// 	if time.Since(tm) > 1000*time.Millisecond {
	// 		err := a.messages.Update(ctx, *assistantMsg)
	// 		assistantMsg.UpdatedAt = time.Now().Unix()
	// 		return err
	// 	}
	case provider.EventToolUseStop:
		assistantMsg.FinishToolCall(event.ToolCall.ID)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventError:
		if errors.Is(event.Error, context.Canceled) {
			logging.InfoPersist(fmt.Sprintf("Event processing canceled for session: %s", sessionID))
			return context.Canceled
		}
		logging.ErrorPersist(event.Error.Error())
		return event.Error
	case provider.EventComplete:
		calls := append([]message.ToolCall(nil), event.Response.ToolCalls...)
		for i := range calls {
			calls[i].Origin = ""
		}
		assistantMsg.SetToolCalls(calls)
		assistantMsg.AddFinish(event.Response.FinishReason)
		assistantMsg.SetProviderState(event.Response.ProviderState)
		if err := a.messages.Update(ctx, *assistantMsg); err != nil {
			return fmt.Errorf("failed to update message: %w", err)
		}
		a.observeInputUsage(sessionID, a.provider.Model(), event.Response.Usage)
		return a.TrackUsage(ctx, sessionID, a.provider.Model(), event.Response.Usage)
	}

	return nil
}

func (a *agent) TrackUsage(ctx context.Context, sessionID string, model models.Model, usage provider.TokenUsage) error {
	a.observeUsage(sessionID, usage, model.ID)
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		model.CostPer1MIn/1e6*float64(usage.InputTokens) +
		model.CostPer1MOut/1e6*float64(usage.OutputTokens)

	sess.Cost += cost
	sess.CompletionTokens = usage.OutputTokens
	sess.PromptTokens = usage.InputTokens + usage.CacheCreationTokens + usage.CacheReadTokens

	_, err = a.sessions.Save(ctx, sess)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (a *agent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	model, ok := models.SupportedModels[modelID]
	if !ok {
		return models.Model{}, fmt.Errorf("model %s not supported", modelID)
	}
	return a.SelectModel(agentName, model)
}

func (a *agent) SelectModel(agentName config.AgentName, model models.Model) (models.Model, error) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.IsBusy() {
		return models.Model{}, fmt.Errorf("cannot change model while processing requests")
	}

	model, candidate, err := config.ModelSelection(agentName, model)
	if err != nil {
		return models.Model{}, err
	}
	provider, err := createAgentProviderFor(agentName, candidate, model)
	if err != nil {
		return models.Model{}, fmt.Errorf("failed to create provider for model %s: %w", model.ID, err)
	}
	if err := config.UpdateAgentModelDefinition(agentName, model); err != nil {
		return models.Model{}, err
	}
	ownRole := a.agentName
	if ownRole == "" {
		ownRole = config.AgentCoder
	}
	if agentName == ownRole {
		a.provider = provider
		a.systemMessage, a.maxOutputTokens = configuredPrompt(agentName, model)
		a.contextMu.Lock()
		a.contextStatuses = nil
		a.contextMu.Unlock()
	} else {
		switch agentName {
		case config.AgentTitle:
			a.titleProvider = provider
		case config.AgentSummarizer:
			a.summarizeProvider = provider
		case config.AgentTask:
			// Task agents are constructed on demand from persisted configuration.
		}
	}
	return provider.Model(), nil
}

func (a *agent) Summarize(ctx context.Context, sessionID string) error {
	if a.summarizeProvider == nil {
		return fmt.Errorf("summarize provider not available")
	}

	// Check if session is busy
	a.runMu.Lock()
	if a.closed {
		a.runMu.Unlock()
		return fmt.Errorf("agent is closed")
	}
	if a.IsSessionBusy(sessionID) {
		a.runMu.Unlock()
		return ErrSessionBusy
	}

	// Create a new context with cancellation
	summarizeCtx, cancel := context.WithCancel(ctx)

	// Store the cancel function in activeRequests to allow cancellation
	a.activeRequests.Store(sessionID+"-summarize", cancel)
	a.runMu.Unlock()

	go func() {
		defer a.activeRequests.Delete(sessionID + "-summarize")
		defer cancel()
		event := AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Starting summarization...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		// Get all messages from the session
		msgs, err := a.messages.List(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to list messages: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		summarizeCtx = context.WithValue(summarizeCtx, tools.SessionIDContextKey, sessionID)

		if len(msgs) == 0 {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("no messages to summarize"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Analyzing conversation...",
		}
		a.Publish(pubsub.CreatedEvent, event)

		// Add a system message to guide the summarization
		summarizePrompt := "Provide a detailed but concise summary of our conversation above. Focus on information that would be helpful for continuing the conversation, including what we did, what we're doing, which files we're working on, and what we're going to do next."

		// Create a new message with the summarize prompt
		promptMsg := message.Message{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: summarizePrompt}},
		}

		// Append the prompt to the messages
		msgsWithPrompt := append(msgs, promptMsg)

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Generating summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		// Send the messages to the summarize provider
		budgetHistory, budgetErr := auxiliaryBudget(config.AgentSummarizer, a.summarizeProvider, msgsWithPrompt)
		var response *provider.ProviderResponse
		err = budgetErr
		if err == nil {
			response, err = timedProvider(a.summarizeProvider).SendMessages(summarizeCtx, budgetHistory, nil)
		}
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to summarize: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		summary := strings.TrimSpace(response.Content)
		if summary == "" {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("empty summary returned"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Creating new session...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		oldSession, err := a.sessions.Get(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to get session: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		// Create a message in the new session with the summary
		msg, err := a.messages.Create(summarizeCtx, oldSession.ID, message.CreateMessageParams{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: summary},
				message.Finish{
					Reason: message.FinishReasonEndTurn,
					Time:   time.Now().Unix(),
				},
			},
			Model: a.summarizeProvider.Model().ID,
		})
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to create summary message: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		oldSession.SummaryMessageID = msg.ID
		oldSession.CompletionTokens = response.Usage.OutputTokens
		oldSession.PromptTokens = 0
		model := a.summarizeProvider.Model()
		usage := response.Usage
		cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
			model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
			model.CostPer1MIn/1e6*float64(usage.InputTokens) +
			model.CostPer1MOut/1e6*float64(usage.OutputTokens)
		oldSession.Cost += cost
		_, err = a.sessions.Save(summarizeCtx, oldSession)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to save session: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
		}

		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: oldSession.ID,
			Progress:  "Summary complete",
			Done:      true,
		}
		a.Publish(pubsub.CreatedEvent, event)
		// Send final success event with the new session ID
	}()

	return nil
}

func createAgentProvider(agentName config.AgentName) (provider.Provider, error) {
	cfg := config.Get()
	agentConfig, ok := cfg.Agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentName)
	}
	model, ok := models.SupportedModels[agentConfig.Model]
	if !ok {
		return nil, fmt.Errorf("model %s not supported", agentConfig.Model)
	}
	return createAgentProviderFor(agentName, agentConfig, model)
}

func createAgentProviderFor(agentName config.AgentName, agentConfig config.Agent, model models.Model) (provider.Provider, error) {
	model = config.ModelWithProviderDefaults(model)
	adaptiveWindow := false
	if learned := learnedContext(model); learned >= 4096 && (model.ContextWindow <= 0 || learned < model.ContextWindow) {
		model.ContextWindow = learned
		model.ContextSource = "learned after provider context/memory failure"
		adaptiveWindow = true
	}

	providerCfg, err := config.ProviderForModel(model)
	if err != nil {
		return nil, err
	}
	if providerCfg.Disabled {
		return nil, fmt.Errorf("provider %s is not enabled", model.Provider)
	}
	maxTokens := model.DefaultMaxTokens
	if agentConfig.MaxTokens > 0 {
		maxTokens = min(agentConfig.MaxTokens, model.ContextWindow/2)
	}
	if adaptiveWindow && model.ContextWindow > 0 {
		maxTokens = min(maxTokens, max(int64(512), model.ContextWindow/4))
	}
	opts := []provider.ProviderClientOption{
		provider.WithAPIKey(providerCfg.APIKey),
		provider.WithTLSInsecure(providerCfg.TLSInsecure),
		provider.WithModel(model),
		provider.WithSystemMessage(prompt.GetAgentPrompt(agentName, model.Provider)),
		provider.WithMaxTokens(maxTokens),
	}
	if providerCfg.BaseURL != "" {
		opts = append(opts, provider.WithBaseURL(providerCfg.BaseURL))
	}
	if model.CanReason && (model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal || model.Provider == models.ProviderAzure || model.Provider == models.ProviderGROQ || model.Provider == models.ProviderOpenRouter || model.Provider == models.ProviderXAI) {
		opts = append(
			opts,
			provider.WithOpenAIOptions(
				provider.WithReasoningEffort(agentConfig.ReasoningEffort),
			),
		)
	} else if model.Provider == models.ProviderAnthropic && model.CanReason && agentName == config.AgentCoder {
		opts = append(
			opts,
			provider.WithAnthropicOptions(
				provider.WithAnthropicShouldThinkFn(provider.DefaultShouldThinkFn),
			),
		)
	}
	agentProvider, err := provider.NewProvider(
		model.Provider,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create provider: %v", err)
	}

	return agentProvider, nil
}

func (a *agent) Close() error {
	a.runMu.Lock()
	a.closed = true
	runs := make([]*generationRun, 0, len(a.runs))
	for _, run := range a.runs {
		runs = append(runs, run)
	}
	a.runMu.Unlock()
	a.activeRequests.Range(func(key, _ any) bool {
		a.Cancel(strings.TrimSuffix(key.(string), "-summarize"))
		return true
	})
	var result error
	for _, tool := range a.tools {
		if closer, ok := tool.(interface{ Close() error }); ok {
			result = errors.Join(result, closer.Close())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, run := range runs {
		select {
		case <-run.done:
		case <-ctx.Done():
			return errors.Join(result, fmt.Errorf("agent shutdown timed out"))
		}
	}
	return result
}
