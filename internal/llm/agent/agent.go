package agent

import (
	"context"
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
	a.activeRequests.Range(func(key, value any) bool {
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
	response, err := sendAuxiliaryRequest(ctx, config.AgentTitle, titleProvider, []message.Message{{Role: message.User, Parts: parts}}, nil)
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
	history, err := a.prepareGenerationHistory(ctx, sessionID, content, attachmentParts, run)
	if err != nil {
		return a.err(err)
	}
	g := generationState{agent: a, ctx: ctx, sessionID: sessionID, run: run, history: history, signatures: make([][32]byte, 0, 10)}
	stopPreparation()
	for {
		if done, event := g.step(); done {
			return event
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
	requestHistory, prepared, err := a.prepareStreamRequest(ctx, sessionID, msgHistory, run)
	if err != nil {
		return assistantMsg, nil, err
	}
	stopContext()
	providerStarted := time.Now()
	stopProvider := a.startPhase(sessionID, "provider")
	defer stopProvider()
	streamModel := runProvider(a, run).Model()
	streamUsage := a.startStreamUsage(sessionID, streamModel.ID, streamModel)
	defer a.finishStreamUsage(sessionID, streamUsage)
	streamProvider := runProvider(a, run)
	if prepared != nil {
		streamProvider = &preparedStreamProvider{Provider: streamProvider, prepared: prepared}
	}
	eventChan := timedProvider(streamProvider).StreamResponse(ctx, requestHistory, a.exposedTools(run))
	assistantMsg, err = a.messages.Create(ctx, sessionID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{}, Model: a.provider.Model().ID})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}
	defer a.preserveInterruptedStream(sessionID, &assistantMsg, &toolMsg, &failure)
	ctx = a.toolExecutionContext(ctx, sessionID, assistantMsg.ID)
	failure = a.readAssistantEvents(ctx, sessionID, &assistantMsg, eventChan, providerStarted, streamUsage)
	stopProvider()
	a.finishStreamUsage(sessionID, streamUsage)
	if failure != nil {
		return assistantMsg, nil, failure
	}
	if err := a.markAssistantCitation(ctx, msgHistory, run, &assistantMsg); err != nil {
		return assistantMsg, nil, err
	}
	toolMsg, failure = a.executeToolBatch(ctx, sessionID, &assistantMsg, run)
	return assistantMsg, toolMsg, failure
}

func (a *agent) finishMessage(ctx context.Context, msg *message.Message, finishReson message.FinishReason) {
	msg.AddFinish(finishReson)
	_ = a.messages.Update(ctx, *msg)
}

// processEvent handles structural/terminal events. Text and reasoning fragments
// belong to the stream writer, which materializes them before these transitions.
func (a *agent) processEvent(ctx context.Context, sessionID string, assistantMsg *message.Message, event provider.ProviderEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case provider.EventToolUseStart:
		call := *event.ToolCall
		call.Origin = "" // Provider payloads cannot assign application-owned provenance.
		assistantMsg.AddToolCall(call)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventToolUseStop:
		assistantMsg.FinishToolCall(event.ToolCall.ID)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventError:
		if errors.Is(event.Error, context.Canceled) {
			logging.InfoPersist(fmt.Sprintf("Event processing canceled for session: %s", sessionID))
			return context.Canceled
		}
		failure := publicProviderFailure(event.Error, a.provider.Model())
		logging.ErrorPersist(failure.Error())
		return failure
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

	go a.summarizeSession(summarizeCtx, sessionID, cancel)

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

func (a *agent) summarizeSession(ctx context.Context, sessionID string, cancel context.CancelFunc) {
	defer a.activeRequests.Delete(sessionID + "-summarize")
	defer cancel()
	event := AgentEvent{
		Type:     AgentEventTypeSummarize,
		Progress: "Starting summarization...",
	}

	a.Publish(pubsub.CreatedEvent, event)
	// Get all messages from the session
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		event = AgentEvent{
			Type:  AgentEventTypeError,
			Error: fmt.Errorf("failed to list messages: %w", err),
			Done:  true,
		}
		a.Publish(pubsub.CreatedEvent, event)
		return
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)

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
	response, err := sendAuxiliaryRequest(ctx, config.AgentSummarizer, a.summarizeProvider, msgsWithPrompt, nil)
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
	oldSession, err := a.sessions.Get(ctx, sessionID)
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
	msg, err := a.messages.Create(ctx, oldSession.ID, message.CreateMessageParams{
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
	_, err = a.sessions.Save(ctx, oldSession)
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
}
