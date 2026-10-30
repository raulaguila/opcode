package provider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type EventType string

// ErrInvalidFinalAnswer identifies a malformed private reply, before any tool
// execution. Retrying generation must never replay previous completed effects.
var ErrInvalidFinalAnswer = errors.New("invalid final answer protocol")

const maxRetries = 8

const (
	EventContentStart  EventType = "content_start"
	EventToolUseStart  EventType = "tool_use_start"
	EventToolUseDelta  EventType = "tool_use_delta"
	EventToolUseStop   EventType = "tool_use_stop"
	EventContentDelta  EventType = "content_delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventContentStop   EventType = "content_stop"
	EventComplete      EventType = "complete"
	EventError         EventType = "error"
	EventWarning       EventType = "warning"
)

type TokenUsage struct {
	OutputDurationNS    int64
	DurationSource      string
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

type ProviderResponse struct {
	Content       string
	ToolCalls     []message.ToolCall
	Usage         TokenUsage
	FinishReason  message.FinishReason
	ProviderState *message.ProviderState
}

type ProviderEvent struct {
	Type EventType

	Content  string
	Thinking string
	Response *ProviderResponse
	ToolCall *message.ToolCall
	Error    error
}
type Provider interface {
	SendMessages(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error)

	StreamResponse(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent

	Model() models.Model
}

type providerClientOptions struct {
	tlsInsecure     bool
	apiKey          string
	baseURL         string
	model           models.Model
	maxTokens       int64
	systemMessage   string
	sourceCitations []string

	anthropicOptions []AnthropicOption
	openaiOptions    []OpenAIOption
	geminiOptions    []GeminiOption
	bedrockOptions   []BedrockOption
	copilotOptions   []CopilotOption
}

type ProviderClientOption func(*providerClientOptions)

func WithTLSInsecure(enabled bool) ProviderClientOption {
	return func(o *providerClientOptions) { o.tlsInsecure = enabled }
}

type providerClient interface {
	send(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error)
	stream(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent
}

type baseProvider struct {
	options providerClientOptions
	client  providerClient
}

func NewProvider(providerName models.ModelProvider, opts ...ProviderClientOption) (Provider, error) {
	options := providerClientOptions{}
	for _, apply := range opts {
		apply(&options)
	}
	if err := validateInsecureTLS(providerName, options); err != nil {
		return nil, err
	}
	if options.baseURL != "" {
		options.openaiOptions = append(options.openaiOptions, WithOpenAIBaseURL(options.baseURL))
	}
	client, options, err := createProviderClient(providerName, options)
	if err != nil {
		return nil, err
	}
	return &baseProvider{options: options, client: client}, nil
}

func (p *baseProvider) cleanMessages(messages []message.Message) (cleaned []message.Message) {
	for _, msg := range messages {
		// The message has no content
		if len(msg.Parts) == 0 {
			continue
		}
		cleaned = append(cleaned, msg)
	}
	return
}

func (p *baseProvider) SendMessages(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error) {
	messages = p.cleanMessages(messages)
	return p.client.send(ctx, messages, tools)
}

func (p *baseProvider) Model() models.Model {
	return p.options.model
}

func (p *baseProvider) StreamResponse(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent {
	messages = p.cleanMessages(messages)
	return p.client.stream(ctx, messages, tools)
}

func WithAPIKey(apiKey string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.apiKey = apiKey
	}
}

func WithBaseURL(baseURL string) ProviderClientOption {
	return func(options *providerClientOptions) { options.baseURL = baseURL }
}

func WithModel(model models.Model) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.model = model
	}
}

func WithMaxTokens(maxTokens int64) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.maxTokens = maxTokens
	}
}

func WithSystemMessage(systemMessage string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.systemMessage = systemMessage
	}
}

func WithAnthropicOptions(anthropicOptions ...AnthropicOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.anthropicOptions = anthropicOptions
	}
}

func WithOpenAIOptions(openaiOptions ...OpenAIOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.openaiOptions = openaiOptions
	}
}

func WithGeminiOptions(geminiOptions ...GeminiOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.geminiOptions = geminiOptions
	}
}

func WithBedrockOptions(bedrockOptions ...BedrockOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.bedrockOptions = bedrockOptions
	}
}

func WithCopilotOptions(copilotOptions ...CopilotOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.copilotOptions = copilotOptions
	}
}

func validateInsecureTLS(name models.ModelProvider, options providerClientOptions) error {
	if !options.tlsInsecure {
		return nil
	}
	u, err := url.Parse(options.baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || (name != models.ProviderLocal && name != models.ProviderOpenAI) || (options.model.API != "" && options.model.API != "chat_completions" && options.model.API != "responses") {
		return fmt.Errorf("insecure TLS requires an explicit HTTPS OpenAI-compatible connection")
	}
	return nil
}
func createProviderClient(name models.ModelProvider, options providerClientOptions) (providerClient, providerClientOptions, error) {
	switch name {
	case models.ProviderCopilot:
		client, err := newCopilotClient(options)
		return client, options, err
	case models.ProviderAnthropic:
		return newAnthropicClient(options), options, nil
	case models.ProviderOpenAI:
		return newOpenAIClient(options), options, nil
	case models.ProviderGemini:
		client, err := newGeminiClient(options)
		return client, options, err
	case models.ProviderBedrock:
		client, err := newBedrockClient(options)
		return client, options, err
	case models.ProviderAzure:
		client, err := newAzureClient(options)
		return client, options, err
	case models.ProviderVertexAI:
		client, err := newVertexAIClient(options)
		return client, options, err
	case models.ProviderGROQ:
		options.openaiOptions = append([]OpenAIOption{WithOpenAIBaseURL("https://api.groq.com/openai/v1")}, options.openaiOptions...)
		return newOpenAIClient(options), options, nil
	case models.ProviderOpenRouter:
		options.openaiOptions = append([]OpenAIOption{WithOpenAIBaseURL("https://openrouter.ai/api/v1"), WithOpenAIExtraHeaders(map[string]string{"HTTP-Referer": "https://github.com/raulaguila/opcode", "X-Title": "Opcode"})}, options.openaiOptions...)
		return newOpenAIClient(options), options, nil
	case models.ProviderXAI:
		options.openaiOptions = append([]OpenAIOption{WithOpenAIBaseURL("https://api.x.ai/v1")}, options.openaiOptions...)
		return newOpenAIClient(options), options, nil
	case models.ProviderLocal:
		return createLocalClient(options)
	case models.ProviderMock:
		return nil, options, fmt.Errorf("mock provider is not implemented")
	}
	return nil, options, fmt.Errorf("provider not supported: %s", name)
}
func createLocalClient(options providerClientOptions) (providerClient, providerClientOptions, error) {
	local := openaiOptions{}
	for _, apply := range options.openaiOptions {
		apply(&local)
	}
	endpoint := ""
	if options.model.ProviderProfile == "" {
		endpoint = os.Getenv("LOCAL_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = local.baseURL
	}
	endpoint, err := models.NormalizeLocalEndpoint(endpoint)
	if err != nil {
		return nil, options, err
	}
	if options.model.API == "ollama" {
		return newOllamaClient(options, endpoint), options, nil
	}
	options.openaiOptions = append(options.openaiOptions, WithOpenAIBaseURL(endpoint))
	return newOpenAIClient(options), options, nil
}
