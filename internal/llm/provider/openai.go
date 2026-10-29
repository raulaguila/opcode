package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/raulaguila/opcode/internal/httpclient"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type openaiOptions struct {
	baseURL         string
	disableCache    bool
	reasoningEffort string
	extraHeaders    map[string]string
}
type OpenAIOption func(*openaiOptions)
type openaiClient struct {
	*httpProvider
	providerOptions providerClientOptions
	options         openaiOptions
}

func newOpenAIClient(opts providerClientOptions) *openaiClient {
	options := openaiOptions{baseURL: "https://api.openai.com/v1"}
	for _, apply := range opts.openaiOptions {
		apply(&options)
	}
	c := &openaiClient{providerOptions: opts, options: options}
	transport := newAPITransport(options.baseURL)
	transport.client.Transport = httpclient.Transport(httpclient.WithInsecureTLS(context.Background(), options.baseURL, opts.tlsInsecure), transport.client.Transport)
	if opts.apiKey != "" {
		transport.headers.Set("Authorization", "Bearer "+opts.apiKey)
	}
	for k, v := range options.extraHeaders {
		transport.headers.Set(k, v)
	}
	c.httpProvider = &httpProvider{transport: transport, protocol: c}
	return c
}

func (o *openaiClient) convertMessages(messages []message.Message) []object {
	result := []object{{"role": "system", "content": o.providerOptions.systemMessage}}
	for _, msg := range messages {
		switch msg.Role {
		case message.User:
			parts := []object{{"type": "text", "text": msg.Content().String()}}
			for _, image := range msg.BinaryContent() {
				parts = append(parts, object{"type": "image_url", "image_url": object{"url": image.String(models.ProviderOpenAI)}})
			}
			result = append(result, object{"role": "user", "content": parts})
		case message.Assistant:
			item := object{"role": "assistant", "content": msg.Content().String()}
			var calls []object
			for _, call := range msg.ToolCalls() {
				calls = append(calls, object{"id": call.ID, "type": "function", "function": object{"name": call.Name, "arguments": call.Input}})
			}
			if len(calls) > 0 {
				item["tool_calls"] = calls
			}
			result = append(result, item)
		case message.Tool:
			for _, r := range msg.ToolResults() {
				result = append(result, object{"role": "tool", "tool_call_id": r.ToolCallID, "content": r.Content})
			}
		}
	}
	return result
}

func (o *openaiClient) request(messages []message.Message, tools []tools.BaseTool, stream bool) (string, any, error) {
	if o.providerOptions.model.API == "responses" {
		return "/responses", o.responsesRequest(messages, tools, stream), nil
	}
	params := object{"model": o.providerOptions.model.APIModel, "messages": o.convertMessages(messages), "stream": stream}
	if o.providerOptions.model.CanReason {
		params["max_completion_tokens"] = o.providerOptions.maxTokens
		if o.options.reasoningEffort != "" {
			params["reasoning_effort"] = o.options.reasoningEffort
		}
	} else {
		params["max_tokens"] = o.providerOptions.maxTokens
	}
	var definitions []object
	for _, tool := range tools {
		info := tool.Info()
		definitions = append(definitions, object{"type": "function", "function": object{"name": info.Name, "description": info.Description, "parameters": toolSchema(tool)}})
	}
	if len(definitions) > 0 {
		params["tools"] = definitions
	}
	if stream {
		params["stream_options"] = object{"include_usage": true}
	}
	return "/chat/completions", params, nil
}

type chatCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type chatMessage struct {
	Content   string     `json:"content"`
	ToolCalls []chatCall `json:"tool_calls"`
	Reasoning string     `json:"reasoning_content"`
}
type chatResponse struct {
	Choices []struct {
		Index        int         `json:"index"`
		Message      chatMessage `json:"message"`
		Delta        chatMessage `json:"delta"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage chatUsage       `json:"usage"`
	Error json.RawMessage `json:"error"`
}
type chatUsage struct {
	Prompt     int64 `json:"prompt_tokens"`
	Completion int64 `json:"completion_tokens"`
	Details    struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (u chatUsage) tokens() TokenUsage {
	return TokenUsage{InputTokens: max(0, u.Prompt-u.Details.Cached), OutputTokens: u.Completion, CacheReadTokens: u.Details.Cached}
}

func (o *openaiClient) decodeResponse(raw json.RawMessage) (*ProviderResponse, error) {
	if o.providerOptions.model.API == "responses" {
		return o.decodeResponses(raw)
	}
	var r chatResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if len(r.Error) > 0 && string(r.Error) != "null" {
		return nil, fmt.Errorf("Chat Completions error: %s", r.Error)
	}
	if len(r.Choices) == 0 || r.Choices[0].FinishReason == "" {
		return nil, fmt.Errorf("Chat Completions returned no completed choice")
	}
	return chatResultFrom(r.Choices[0].Message, r.Choices[0].FinishReason, r.Usage)
}
func chatResultFrom(msg chatMessage, finish string, usage chatUsage) (*ProviderResponse, error) {
	result := &ProviderResponse{Content: msg.Content, Usage: usage.tokens(), FinishReason: chatFinishReason(finish)}
	for _, call := range msg.ToolCalls {
		if finish != "tool_calls" || call.ID == "" || call.Function.Name == "" || !json.Valid([]byte(call.Function.Arguments)) {
			return nil, fmt.Errorf("Chat Completions returned an incomplete function call")
		}
		result.ToolCalls = append(result.ToolCalls, message.ToolCall{ID: call.ID, Name: call.Function.Name, Input: call.Function.Arguments, Type: "function", Finished: true})
	}
	if len(result.ToolCalls) > 0 {
		result.FinishReason = message.FinishReasonToolUse
	}
	return result, nil
}
func chatFinishReason(reason string) message.FinishReason {
	switch reason {
	case "stop":
		return message.FinishReasonEndTurn
	case "length":
		return message.FinishReasonMaxTokens
	case "tool_calls":
		return message.FinishReasonToolUse
	default:
		return message.FinishReasonUnknown
	}
}

func (o *openaiClient) readStream(ctx context.Context, r io.Reader, events chan<- ProviderEvent) error {
	if o.providerOptions.model.API == "responses" {
		return o.readResponsesStream(ctx, r, events)
	}
	var text strings.Builder
	calls := make(map[int]*chatCall)
	var usage chatUsage
	finish := ""
	err := readSSE(r, func(_ string, raw json.RawMessage) (bool, error) {
		if string(raw) == "[DONE]" {
			return true, nil
		}
		var chunk chatResponse
		if err := json.Unmarshal(raw, &chunk); err != nil {
			return false, err
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return false, fmt.Errorf("Chat Completions stream error: %s", chunk.Error)
		}
		if chunk.Usage.Prompt != 0 || chunk.Usage.Completion != 0 {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
			delta := choice.Delta
			text.WriteString(delta.Content)
			if delta.Content != "" && !publishEvent(ctx, events, ProviderEvent{Type: EventContentDelta, Content: delta.Content}) {
				return false, ctx.Err()
			}
			if delta.Reasoning != "" && !publishEvent(ctx, events, ProviderEvent{Type: EventThinkingDelta, Content: delta.Reasoning, Thinking: delta.Reasoning}) {
				return false, ctx.Err()
			}
			for _, part := range delta.ToolCalls {
				call := calls[part.Index]
				if call == nil {
					call = &chatCall{Index: part.Index}
					calls[part.Index] = call
				}
				call.ID += part.ID
				call.Type += part.Type
				call.Function.Name += part.Function.Name
				call.Function.Arguments += part.Function.Arguments
			}
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if finish == "" {
		return fmt.Errorf("Chat Completions stream ended without a finish reason")
	}
	msg := chatMessage{Content: text.String()}
	// Tool indexes are contiguous, and order must remain stable for parallel calls.
	for index := 0; index < len(calls); index++ {
		call := calls[index]
		if call == nil {
			return fmt.Errorf("invalid tool call index")
		}
		msg.ToolCalls = append(msg.ToolCalls, *call)
	}
	result, err := chatResultFrom(msg, finish, usage)
	if err != nil {
		return err
	}
	if !publishEvent(ctx, events, ProviderEvent{Type: EventComplete, Response: result}) {
		return ctx.Err()
	}
	return nil
}

func WithOpenAIBaseURL(baseURL string) OpenAIOption {
	return func(o *openaiOptions) { o.baseURL = baseURL }
}
func WithOpenAIExtraHeaders(headers map[string]string) OpenAIOption {
	return func(o *openaiOptions) { o.extraHeaders = headers }
}
func WithOpenAIDisableCache() OpenAIOption { return func(o *openaiOptions) { o.disableCache = true } }
func WithReasoningEffort(effort string) OpenAIOption {
	return func(o *openaiOptions) {
		switch effort {
		case "none", "minimal", "low", "medium", "high", "xhigh":
			o.reasoningEffort = effort
		default:
			o.reasoningEffort = ""
		}
	}
}
