package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

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
		blocks := a.messageBlocks(msg, i > len(messages)-3 && !a.options.disableCache)
		if len(blocks) == 0 {
			continue
		}
		role := "user"
		if msg.Role == message.Assistant {
			role = "assistant"
		}
		result = append(result, object{"role": role, "content": blocks})
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
	think := a.messagesNeedThinking(messages)
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
		return "", nil, fmt.Errorf("provider Anthropic requires at least one message")
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
		return nil, fmt.Errorf("provider Anthropic API error: %s", r.Error)
	}
	if r.StopReason == "" {
		return nil, fmt.Errorf("provider Anthropic returned no stop reason")
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
		if err := appendAnthropicBlock(result, rawBlock, r.StopReason); err != nil {
			return nil, err
		}
	}
	result.ProviderState = newProviderState(a.providerOptions, "messages", r.Content)
	return result, nil
}
func (a *anthropicClient) readStream(ctx context.Context, r io.Reader, events chan<- ProviderEvent) error {
	s := anthropicStreamState{ctx: ctx, events: events}
	var err error
	if a.bedrock != nil {
		err = readBedrockEvents(r, s.consume)
	} else {
		err = readSSE(r, s.consume)
	}
	if err != nil {
		return err
	}
	response, err := s.finalResponse()
	if err != nil {
		return err
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

func (a *anthropicClient) messageBlocks(msg message.Message, cache bool) []any {
	switch msg.Role {
	case message.User:
		return anthropicUserBlocks(msg, cache)
	case message.Assistant:
		return a.assistantBlocks(msg, cache)
	case message.Tool:
		var blocks []any
		for _, r := range msg.ToolResults() {
			blocks = append(blocks, object{"type": "tool_result", "tool_use_id": r.ToolCallID, "content": r.Content, "is_error": r.IsError})
		}
		return blocks
	}
	return nil
}
func anthropicTextBlock(text string, cache bool) object {
	block := object{"type": "text", "text": text}
	if cache {
		block["cache_control"] = object{"type": "ephemeral"}
	}
	return block
}
func anthropicUserBlocks(msg message.Message, cache bool) []any {
	blocks := []any{anthropicTextBlock(msg.Content().String(), cache)}
	for _, image := range msg.BinaryContent() {
		blocks = append(blocks, object{"type": "image", "source": object{"type": "base64", "media_type": image.MIMEType, "data": base64ImageBytes(image.Data)}})
	}
	return blocks
}
func (a *anthropicClient) assistantBlocks(msg message.Message, cache bool) []any {
	var blocks []any
	if state := providerState(msg, a.providerOptions, "messages"); state != nil {
		var native []json.RawMessage
		if json.Unmarshal(state, &native) == nil && len(native) > 0 {
			for _, block := range native {
				blocks = append(blocks, block)
			}
		}
	}
	if len(blocks) > 0 {
		return blocks
	}
	if text := msg.Content().String(); text != "" {
		blocks = append(blocks, anthropicTextBlock(text, cache))
	}
	for _, call := range msg.ToolCalls() {
		if json.Valid([]byte(call.Input)) {
			blocks = append(blocks, object{"type": "tool_use", "id": call.ID, "name": call.Name, "input": json.RawMessage(call.Input)})
		}
	}
	return blocks
}

func (a *anthropicClient) messagesNeedThinking(messages []object) bool {
	think := a.providerOptions.model.ThinkingMode == "enabled"
	for _, msg := range messages {
		think = anthropicSignedThinking(msg) || think
	}
	return a.requestedThinking(messages) || think
}

type anthropicStreamEvent struct {
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
type anthropicStreamState struct {
	ctx              context.Context
	events           chan<- ProviderEvent
	content          []object
	inputs           []string
	closed           []bool
	response         anthropicResponse
	started, stopped bool
}

func (s *anthropicStreamState) consume(_ string, raw json.RawMessage) (bool, error) {
	var e anthropicStreamEvent
	if err := decodeObject(raw, &e); err != nil {
		return false, err
	}
	switch e.Type {
	case "error":
		return false, fmt.Errorf("provider Anthropic stream error: %s", e.Error)
	case "message_start":
		return false, s.startMessage(e)
	case "content_block_start":
		return false, s.startBlock(e)
	case "content_block_delta":
		return false, s.deltaBlock(e)
	case "content_block_stop":
		return false, s.stopBlock(e)
	case "message_delta":
		s.response.StopReason = e.Delta.StopReason
		mergeAnthropicUsage(&s.response.Usage, e.Usage)
	case "message_stop":
		s.stopped = true
		return true, nil
	}
	return false, nil
}
func (s *anthropicStreamState) startMessage(e anthropicStreamEvent) error {

	if s.started {
		return fmt.Errorf("duplicate message_start")
	}
	s.started = true
	s.response = e.Message

	return nil
}
func (s *anthropicStreamState) startBlock(e anthropicStreamEvent) error {

	if !s.started || e.Index != len(s.content) || e.ContentBlock == nil {
		return fmt.Errorf("invalid Anthropic block start")
	}
	s.content = append(s.content, e.ContentBlock)
	s.inputs = append(s.inputs, "")
	s.closed = append(s.closed, false)
	if e.ContentBlock["type"] == "tool_use" {
		call := message.ToolCall{ID: stringField(e.ContentBlock, "id"), Name: stringField(e.ContentBlock, "name"), Type: "function"}
		if !publishEvent(s.ctx, s.events, ProviderEvent{Type: EventToolUseStart, ToolCall: &call}) {
			return s.ctx.Err()
		}
	}

	return nil
}
func (s *anthropicStreamState) deltaBlock(e anthropicStreamEvent) error {

	if e.Index < 0 || e.Index >= len(s.content) || s.closed[e.Index] {
		return fmt.Errorf("invalid Anthropic block index")
	}
	b := s.content[e.Index]
	switch e.Delta.Type {
	case "text_delta":
		b["text"] = stringField(b, "text") + e.Delta.Text
		if !publishEvent(s.ctx, s.events, ProviderEvent{Type: EventContentDelta, Content: e.Delta.Text}) {
			return s.ctx.Err()
		}
	case "thinking_delta":
		b["thinking"] = stringField(b, "thinking") + e.Delta.Thinking
		if !publishEvent(s.ctx, s.events, ProviderEvent{Type: EventThinkingDelta, Content: e.Delta.Thinking, Thinking: e.Delta.Thinking}) {
			return s.ctx.Err()
		}
	case "signature_delta":
		b["signature"] = stringField(b, "signature") + e.Delta.Signature
	case "input_json_delta":
		s.inputs[e.Index] += e.Delta.PartialJSON
	}

	return nil
}
func (s *anthropicStreamState) stopBlock(e anthropicStreamEvent) error {

	if e.Index < 0 || e.Index >= len(s.content) || s.closed[e.Index] {
		return fmt.Errorf("invalid Anthropic block stop")
	}
	b := s.content[e.Index]
	s.closed[e.Index] = true
	if s.inputs[e.Index] != "" {
		if !json.Valid([]byte(s.inputs[e.Index])) {
			return fmt.Errorf("invalid Anthropic tool JSON")
		}
		b["input"] = json.RawMessage(s.inputs[e.Index])
	}
	if b["type"] == "tool_use" {
		call := message.ToolCall{ID: stringField(b, "id"), Finished: true}
		if !publishEvent(s.ctx, s.events, ProviderEvent{Type: EventToolUseStop, ToolCall: &call}) {
			return s.ctx.Err()
		}
	}

	return nil
}
func (s *anthropicStreamState) finalResponse() (anthropicResponse, error) {
	if !s.started || !s.stopped {
		return s.response, fmt.Errorf("provider Anthropic stream ended without message_stop")
	}
	for i, block := range s.content {
		if !s.closed[i] {
			return s.response, fmt.Errorf("provider Anthropic stream ended with an open content block")
		}
		raw, err := json.Marshal(block)
		if err != nil {
			return s.response, err
		}
		s.response.Content = append(s.response.Content, raw)
	}
	return s.response, nil
}

func appendAnthropicBlock(result *ProviderResponse, rawBlock json.RawMessage, stopReason string) error {

	var b struct {
		Type, Text, ID, Name string
		Input                json.RawMessage
	}
	if err := json.Unmarshal(rawBlock, &b); err != nil {
		return err
	}
	if b.Type == "text" {
		result.Content += b.Text
	}
	if b.Type == "tool_use" {
		if stopReason != "tool_use" || b.ID == "" || b.Name == "" || !json.Valid(b.Input) {
			return fmt.Errorf("provider Anthropic returned an incomplete tool call")
		}
		result.ToolCalls = append(result.ToolCalls, message.ToolCall{ID: b.ID, Name: b.Name, Input: string(b.Input), Type: "function", Finished: true})
	}
	return nil
}

func anthropicSignedThinking(msg object) bool {
	if msg["role"] != "assistant" {
		return false
	}
	raw, _ := json.Marshal(msg["content"])
	var blocks []struct{ Type string }
	_ = json.Unmarshal(raw, &blocks)
	for _, block := range blocks {
		if block.Type == "thinking" || block.Type == "redacted_thinking" {
			return true
		}
	}
	return false
}
func (a *anthropicClient) requestedThinking(messages []object) bool {
	if len(messages) == 0 || a.options.shouldThink == nil {
		return false
	}
	last := messages[len(messages)-1]
	if last["role"] != "user" {
		return false
	}
	raw, _ := json.Marshal(last["content"])
	var blocks []struct{ Type, Text string }
	_ = json.Unmarshal(raw, &blocks)
	think := false
	for _, block := range blocks {
		if block.Type == "text" && a.options.shouldThink(block.Text) {
			think = true
		}
	}
	return think
}
