package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type anthropicOptions struct {
	disableCache bool
	shouldThink  func(string) bool
}
type AnthropicOption func(*anthropicOptions)
type anthropicClient struct {
	*httpProvider
	providerOptions providerClientOptions
	options         anthropicOptions
	bedrock         *bedrockProtocol
}

func newAnthropicClient(opts providerClientOptions) *anthropicClient {
	c := &anthropicClient{providerOptions: opts}
	for _, apply := range opts.anthropicOptions {
		apply(&c.options)
	}
	endpoint := opts.baseURL
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1"
	}
	transport := newAPITransport(endpoint)
	transport.headers.Set("x-api-key", opts.apiKey)
	transport.headers.Set("anthropic-version", "2023-06-01")
	c.httpProvider = &httpProvider{transport: transport, protocol: c}
	return c
}
func (a *anthropicClient) convertMessages(messages []message.Message) []object {
	result := []object{}
	for i, msg := range messages {
		cache := i > len(messages)-3 && !a.options.disableCache
		var blocks []any
		switch msg.Role {
		case message.User:
			text := object{"type": "text", "text": msg.Content().String()}
			if cache {
				text["cache_control"] = object{"type": "ephemeral"}
			}
			blocks = append(blocks, text)
			for _, image := range msg.BinaryContent() {
				blocks = append(blocks, object{"type": "image", "source": object{"type": "base64", "media_type": image.MIMEType, "data": image.String(models.ProviderAnthropic)}})
			}
		case message.Assistant:
			if state := providerState(msg, a.providerOptions, "messages"); state != nil {
				var native []json.RawMessage
				if json.Unmarshal(state, &native) == nil && len(native) > 0 {
					for _, block := range native {
						blocks = append(blocks, block)
					}
				}
			}
			if len(blocks) == 0 {
				if text := msg.Content().String(); text != "" {
					block := object{"type": "text", "text": text}
					if cache {
						block["cache_control"] = object{"type": "ephemeral"}
					}
					blocks = append(blocks, block)
				}
				for _, call := range msg.ToolCalls() {
					if json.Valid([]byte(call.Input)) {
						blocks = append(blocks, object{"type": "tool_use", "id": call.ID, "name": call.Name, "input": json.RawMessage(call.Input)})
					}
				}
			}
		case message.Tool:
			for _, r := range msg.ToolResults() {
				blocks = append(blocks, object{"type": "tool_result", "tool_use_id": r.ToolCallID, "content": r.Content, "is_error": r.IsError})
			}
		}
		if len(blocks) > 0 {
			role := "user"
			if msg.Role == message.Assistant {
				role = "assistant"
			}
			result = append(result, object{"role": role, "content": blocks})
		}
	}
	return result
}
func (a *anthropicClient) convertTools(tools []tools.BaseTool) []object {
	definitions := []object{}
	for i, tool := range tools {
		info := tool.Info()
		item := object{"name": info.Name, "description": info.Description, "input_schema": toolSchema(tool)}
		if i == len(tools)-1 && !a.options.disableCache {
			item["cache_control"] = object{"type": "ephemeral"}
		}
		definitions = append(definitions, item)
	}
	return definitions
}
func (a *anthropicClient) preparedMessages(messages []object, tools []object) object {
	system := object{"type": "text", "text": a.providerOptions.systemMessage}
	if !a.options.disableCache {
		system["cache_control"] = object{"type": "ephemeral"}
	}
	params := object{"model": a.providerOptions.model.APIModel, "max_tokens": a.providerOptions.maxTokens, "messages": messages, "system": []object{system}}
	if len(tools) > 0 {
		params["tools"] = tools
	}
	think := a.providerOptions.model.ThinkingMode == "enabled"
	// Signed thinking blocks must survive the entire tool loop with thinking enabled.
	for _, msg := range messages {
		if msg["role"] == "assistant" {
			raw, _ := json.Marshal(msg["content"])
			var blocks []struct{ Type string }
			_ = json.Unmarshal(raw, &blocks)
			for _, b := range blocks {
				think = think || b.Type == "thinking" || b.Type == "redacted_thinking"
			}
		}
	}
	if len(messages) > 0 && a.options.shouldThink != nil {
		last := messages[len(messages)-1]
		if last["role"] == "user" {
			raw, _ := json.Marshal(last["content"])
			var blocks []struct{ Type, Text string }
			_ = json.Unmarshal(raw, &blocks)
			for _, b := range blocks {
				if b.Type == "text" && a.options.shouldThink(b.Text) {
					think = true
				}
			}
		}
	}
	if a.providerOptions.model.ThinkingMode == "adaptive" {
		params["thinking"] = object{"type": "adaptive"}
	} else if think && a.providerOptions.maxTokens > 1024 {
		params["thinking"] = object{"type": "enabled", "budget_tokens": max(int64(1024), a.providerOptions.maxTokens*4/5)}
	}
	return params
}
func (a *anthropicClient) request(messages []message.Message, tools []tools.BaseTool, stream bool) (string, any, error) {
	params := a.preparedMessages(a.convertMessages(messages), a.convertTools(tools))
	params["stream"] = stream
	if len(messages) == 0 {
		return "", nil, fmt.Errorf("Anthropic requires at least one message")
	}
	if a.bedrock != nil {
		return a.bedrock.request(params, stream)
	}
	return "/messages", params, nil
}

type anthropicResponse struct {
	Content    []json.RawMessage `json:"content"`
	StopReason string            `json:"stop_reason"`
	Usage      anthropicUsage    `json:"usage"`
	Error      json.RawMessage   `json:"error"`
}
type anthropicUsage struct {
	Input   int64 `json:"input_tokens"`
	Output  int64 `json:"output_tokens"`
	Created int64 `json:"cache_creation_input_tokens"`
	Cached  int64 `json:"cache_read_input_tokens"`
}

func (a *anthropicClient) decodeResponse(raw json.RawMessage) (*ProviderResponse, error) {
	var r anthropicResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if len(r.Error) > 0 && string(r.Error) != "null" {
		return nil, fmt.Errorf("Anthropic API error: %s", r.Error)
	}
	if r.StopReason == "" {
		return nil, fmt.Errorf("Anthropic returned no stop reason")
	}
	result := &ProviderResponse{FinishReason: message.FinishReasonUnknown, Usage: TokenUsage{InputTokens: r.Usage.Input, OutputTokens: r.Usage.Output, CacheCreationTokens: r.Usage.Created, CacheReadTokens: r.Usage.Cached}}
	switch r.StopReason {
	case "end_turn", "stop_sequence":
		result.FinishReason = message.FinishReasonEndTurn
	case "max_tokens":
		result.FinishReason = message.FinishReasonMaxTokens
	case "tool_use":
		result.FinishReason = message.FinishReasonToolUse
	}
	for _, rawBlock := range r.Content {
		var b struct {
			Type, Text, ID, Name string
			Input                json.RawMessage
		}
		if err := json.Unmarshal(rawBlock, &b); err != nil {
			return nil, err
		}
		if b.Type == "text" {
			result.Content += b.Text
		}
		if b.Type == "tool_use" {
			if r.StopReason != "tool_use" || b.ID == "" || b.Name == "" || !json.Valid(b.Input) {
				return nil, fmt.Errorf("Anthropic returned an incomplete tool call")
			}
			result.ToolCalls = append(result.ToolCalls, message.ToolCall{ID: b.ID, Name: b.Name, Input: string(b.Input), Type: "function", Finished: true})
		}
	}
	result.ProviderState = newProviderState(a.providerOptions, "messages", r.Content)
	return result, nil
}
func (a *anthropicClient) readStream(ctx context.Context, r io.Reader, events chan<- ProviderEvent) error {
	var content []object
	var inputs []string
	var closed []bool
	response := anthropicResponse{}
	started, stopped := false, false
	consume := func(_ string, raw json.RawMessage) (bool, error) {
		var e struct {
			Type         string
			Index        int
			Message      anthropicResponse
			ContentBlock object `json:"content_block"`
			Delta        struct {
				Type, Text, Thinking, Signature string
				PartialJSON                     string `json:"partial_json"`
				StopReason                      string `json:"stop_reason"`
			}
			Usage object
			Error json.RawMessage
		}
		if err := decodeObject(raw, &e); err != nil {
			return false, err
		}
		switch e.Type {
		case "error":
			return false, fmt.Errorf("Anthropic stream error: %s", e.Error)
		case "message_start":
			if started {
				return false, fmt.Errorf("duplicate message_start")
			}
			started = true
			response = e.Message
		case "content_block_start":
			if !started || e.Index != len(content) || e.ContentBlock == nil {
				return false, fmt.Errorf("invalid Anthropic block start")
			}
			content = append(content, e.ContentBlock)
			inputs = append(inputs, "")
			closed = append(closed, false)
			if e.ContentBlock["type"] == "tool_use" {
				call := message.ToolCall{ID: stringField(e.ContentBlock, "id"), Name: stringField(e.ContentBlock, "name"), Type: "function"}
				if !publishEvent(ctx, events, ProviderEvent{Type: EventToolUseStart, ToolCall: &call}) {
					return false, ctx.Err()
				}
			}
		case "content_block_delta":
			if e.Index < 0 || e.Index >= len(content) || closed[e.Index] {
				return false, fmt.Errorf("invalid Anthropic block index")
			}
			b := content[e.Index]
			switch e.Delta.Type {
			case "text_delta":
				b["text"] = stringField(b, "text") + e.Delta.Text
				if !publishEvent(ctx, events, ProviderEvent{Type: EventContentDelta, Content: e.Delta.Text}) {
					return false, ctx.Err()
				}
			case "thinking_delta":
				b["thinking"] = stringField(b, "thinking") + e.Delta.Thinking
				if !publishEvent(ctx, events, ProviderEvent{Type: EventThinkingDelta, Content: e.Delta.Thinking, Thinking: e.Delta.Thinking}) {
					return false, ctx.Err()
				}
			case "signature_delta":
				b["signature"] = stringField(b, "signature") + e.Delta.Signature
			case "input_json_delta":
				inputs[e.Index] += e.Delta.PartialJSON
			}
		case "content_block_stop":
			if e.Index < 0 || e.Index >= len(content) || closed[e.Index] {
				return false, fmt.Errorf("invalid Anthropic block stop")
			}
			b := content[e.Index]
			closed[e.Index] = true
			if inputs[e.Index] != "" {
				if !json.Valid([]byte(inputs[e.Index])) {
					return false, fmt.Errorf("invalid Anthropic tool JSON")
				}
				b["input"] = json.RawMessage(inputs[e.Index])
			}
			if b["type"] == "tool_use" {
				call := message.ToolCall{ID: stringField(b, "id"), Finished: true}
				if !publishEvent(ctx, events, ProviderEvent{Type: EventToolUseStop, ToolCall: &call}) {
					return false, ctx.Err()
				}
			}
		case "message_delta":
			response.StopReason = e.Delta.StopReason
			// Usage fields in deltas are cumulative, and absent fields must not reset earlier counts.
			mergeAnthropicUsage(&response.Usage, e.Usage)
		case "message_stop":
			stopped = true
			return true, nil
		}
		return false, nil
	}
	var err error
	if a.bedrock != nil {
		err = readBedrockEvents(r, consume)
	} else {
		err = readSSE(r, consume)
	}
	if err != nil {
		return err
	}
	if !started || !stopped {
		return fmt.Errorf("Anthropic stream ended without message_stop")
	}
	for i, block := range content {
		if !closed[i] {
			return fmt.Errorf("Anthropic stream ended with an open content block")
		}
		raw, err := json.Marshal(block)
		if err != nil {
			return err
		}
		response.Content = append(response.Content, raw)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	result, err := a.decodeResponse(raw)
	if err != nil {
		return err
	}
	if !publishEvent(ctx, events, ProviderEvent{Type: EventComplete, Response: result}) {
		return ctx.Err()
	}
	return nil
}
func mergeAnthropicUsage(u *anthropicUsage, delta object) {
	for key, value := range delta {
		number, ok := value.(json.Number)
		if !ok {
			continue
		}
		n, err := number.Int64()
		if err != nil {
			continue
		}
		switch key {
		case "input_tokens":
			u.Input = int64(n)
		case "output_tokens":
			u.Output = int64(n)
		case "cache_creation_input_tokens":
			u.Created = int64(n)
		case "cache_read_input_tokens":
			u.Cached = int64(n)
		}
	}
}
func stringField(o object, key string) string { s, _ := o[key].(string); return s }
func WithAnthropicDisableCache() AnthropicOption {
	return func(o *anthropicOptions) { o.disableCache = true }
}
func DefaultShouldThinkFn(s string) bool { return strings.Contains(strings.ToLower(s), "think") }
func WithAnthropicShouldThinkFn(fn func(string) bool) AnthropicOption {
	return func(o *anthropicOptions) { o.shouldThink = fn }
}
