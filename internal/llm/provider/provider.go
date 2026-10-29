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
	clientOptions := providerClientOptions{}
	for _, o := range opts {
		o(&clientOptions)
	}
	if clientOptions.tlsInsecure {
		u, err := url.Parse(clientOptions.baseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || (providerName != models.ProviderLocal && providerName != models.ProviderOpenAI) || (clientOptions.model.API != "" && clientOptions.model.API != "chat_completions" && clientOptions.model.API != "responses") {
			return nil, fmt.Errorf("insecure TLS requires an explicit HTTPS OpenAI-compatible connection")
		}
	}
	if clientOptions.baseURL != "" {
		clientOptions.openaiOptions = append(clientOptions.openaiOptions, WithOpenAIBaseURL(clientOptions.baseURL))
	}
	switch providerName {
	case models.ProviderCopilot:
		client, err := newCopilotClient(clientOptions)
		if err != nil {
			return nil, err
		}
		return &baseProvider{options: clientOptions, client: client}, nil
	case models.ProviderAnthropic:
		return &baseProvider{
			options: clientOptions,
			client:  newAnthropicClient(clientOptions),
		}, nil
	case models.ProviderOpenAI:
		return &baseProvider{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderGemini:
		client, err := newGeminiClient(clientOptions)
		if err != nil {
			return nil, err
		}
		return &baseProvider{options: clientOptions, client: client}, nil
	case models.ProviderBedrock:
		client, err := newBedrockClient(clientOptions)
		if err != nil {
			return nil, err
		}
		return &baseProvider{options: clientOptions, client: client}, nil
	case models.ProviderGROQ:
		clientOptions.openaiOptions = append([]OpenAIOption{
			WithOpenAIBaseURL("https://api.groq.com/openai/v1"),
		}, clientOptions.openaiOptions...)
		return &baseProvider{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderAzure:
		client, err := newAzureClient(clientOptions)
		if err != nil {
			return nil, err
		}
		return &baseProvider{options: clientOptions, client: client}, nil
	case models.ProviderVertexAI:
		client, err := newVertexAIClient(clientOptions)
		if err != nil {
			return nil, err
		}
		return &baseProvider{options: clientOptions, client: client}, nil
	case models.ProviderOpenRouter:
		clientOptions.openaiOptions = append([]OpenAIOption{
			WithOpenAIBaseURL("https://openrouter.ai/api/v1"),
			WithOpenAIExtraHeaders(map[string]string{
				"HTTP-Referer": "https://github.com/raulaguila/opcode",
				"X-Title":      "Opcode",
			}),
		}, clientOptions.openaiOptions...)
		return &baseProvider{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderXAI:
		clientOptions.openaiOptions = append([]OpenAIOption{
			WithOpenAIBaseURL("https://api.x.ai/v1"),
		}, clientOptions.openaiOptions...)
		return &baseProvider{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderLocal:
		localOptions := openaiOptions{}
		for _, apply := range clientOptions.openaiOptions {
			apply(&localOptions)
		}
		endpoint := ""
		if clientOptions.model.ProviderProfile == "" {
			endpoint = os.Getenv("LOCAL_ENDPOINT")
		}
		if endpoint == "" {
			endpoint = localOptions.baseURL
		}
		endpoint, err := models.NormalizeLocalEndpoint(endpoint)
		if err != nil {
			return nil, err
		}
		if clientOptions.model.API == "ollama" {
			return &baseProvider{options: clientOptions, client: newOllamaClient(clientOptions, endpoint)}, nil
		}
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL(endpoint),
		)
		return &baseProvider{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderMock:
		return nil, fmt.Errorf("mock provider is not implemented")
	}
	return nil, fmt.Errorf("provider not supported: %s", providerName)
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
