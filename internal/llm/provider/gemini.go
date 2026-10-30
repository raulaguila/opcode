package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type geminiOptions struct{ disableCache bool }
type GeminiOption func(*geminiOptions)
type geminiClient struct {
	*httpProvider
	providerOptions providerClientOptions
	options         geminiOptions
	modelPath       string
}

func newGeminiClient(opts providerClientOptions) (*geminiClient, error) {
	if opts.apiKey == "" {
		return nil, fmt.Errorf("provider Gemini requires an API key")
	}
	c := &geminiClient{providerOptions: opts, modelPath: "/models/" + url.PathEscape(strings.TrimPrefix(opts.model.APIModel, "models/"))}
	for _, apply := range opts.geminiOptions {
		apply(&c.options)
	}
	endpoint := opts.baseURL
	if endpoint == "" {
		endpoint = "https://generativelanguage.googleapis.com/v1beta"
	}
	transport := newAPITransport(endpoint)
	transport.headers.Set("x-goog-api-key", opts.apiKey)
	c.httpProvider = &httpProvider{transport: transport, protocol: c}
	return c, nil
}

type geminiContent struct {
	Role  string            `json:"role"`
	Parts []json.RawMessage `json:"parts"`
}
type geminiPart struct {
	Text             string `json:"text,omitempty"`
	Thought          bool   `json:"thought,omitempty"`
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
	FunctionCall     *struct {
		ID   string          `json:"id,omitempty"`
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"functionCall,omitempty"`
}

func rawJSON(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
func (g *geminiClient) convertMessages(messages []message.Message) []geminiContent {
	names, ids := g.messageCallIdentities(messages)
	history := []geminiContent{}
	for _, msg := range messages {
		content := g.messageContent(msg, names, ids)
		if len(content.Parts) > 0 {
			history = append(history, content)
		}
	}
	return history
}
func (g *geminiClient) request(messages []message.Message, tools []tools.BaseTool, stream bool) (string, any, error) {
	contents := g.convertMessages(messages)
	if len(contents) == 0 {
		return "", nil, fmt.Errorf("provider Gemini requires at least one message")
	}
	params := object{"contents": contents, "generationConfig": object{"maxOutputTokens": g.providerOptions.maxTokens}, "systemInstruction": object{"parts": []object{{"text": g.providerOptions.systemMessage}}}}
	var declarations []object
	for _, tool := range tools {
		info := tool.Info()
		declarations = append(declarations, object{"name": info.Name, "description": info.Description, "parametersJsonSchema": toolSchema(tool)})
	}
	if len(declarations) > 0 {
		params["tools"] = []object{{"functionDeclarations": declarations}}
	}
	method := ":generateContent"
	if stream {
		method = ":streamGenerateContent?alt=sse"
	}
	return g.modelPath + method, params, nil
}

type geminiCandidate struct {
	Content      *geminiContent `json:"content"`
	FinishReason string         `json:"finishReason"`
	Index        int            `json:"index"`
}
type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Usage      struct {
		Prompt   int64 `json:"promptTokenCount"`
		Output   int64 `json:"candidatesTokenCount"`
		Cached   int64 `json:"cachedContentTokenCount"`
		Thoughts int64 `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	Error json.RawMessage `json:"error"`
}

func (g *geminiClient) decodeResponse(raw json.RawMessage) (*ProviderResponse, error) {
	var r geminiResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if len(r.Error) > 0 && string(r.Error) != "null" {
		return nil, fmt.Errorf("provider Gemini API error: %s", r.Error)
	}
	if len(r.Candidates) == 0 || r.Candidates[0].Content == nil {
		return nil, fmt.Errorf("provider Gemini returned no candidate content")
	}
	candidate := r.Candidates[0]
	if candidate.FinishReason == "" || candidate.FinishReason == "FINISH_REASON_UNSPECIFIED" {
		return nil, fmt.Errorf("provider Gemini returned an incomplete candidate")
	}
	result := &ProviderResponse{FinishReason: message.FinishReasonUnknown, Usage: TokenUsage{InputTokens: max(0, r.Usage.Prompt-r.Usage.Cached), OutputTokens: r.Usage.Output + r.Usage.Thoughts, CacheReadTokens: r.Usage.Cached}}
	switch candidate.FinishReason {
	case "STOP":
		result.FinishReason = message.FinishReasonEndTurn
	case "MAX_TOKENS":
		result.FinishReason = message.FinishReasonMaxTokens
	}
	for _, rawPart := range candidate.Content.Parts {
		if err := appendGeminiPart(result, rawPart, candidate.FinishReason); err != nil {
			return nil, err
		}
	}
	if len(result.ToolCalls) > 0 {
		result.FinishReason = message.FinishReasonToolUse
	}
	result.ProviderState = newProviderState(g.providerOptions, "generate_content", candidate.Content.Parts)
	return result, nil
}
func (g *geminiClient) readStream(ctx context.Context, r io.Reader, events chan<- ProviderEvent) error {
	s := geminiStreamState{ctx: ctx, events: events}
	if err := readSSE(r, s.consume); err != nil {
		return err
	}
	if s.finish == "" || len(s.parts) == 0 {
		return fmt.Errorf("provider Gemini stream ended without a complete candidate")
	}
	s.final.Candidates = append(s.final.Candidates, geminiCandidate{Content: &geminiContent{Role: "model", Parts: s.parts}, FinishReason: s.finish})
	result, err := g.decodeResponse(rawJSON(s.final))
	if err != nil {
		return err
	}
	if !publishEvent(ctx, events, ProviderEvent{Type: EventComplete, Response: result}) {
		return ctx.Err()
	}
	return nil
}
func WithGeminiDisableCache() GeminiOption { return func(o *geminiOptions) { o.disableCache = true } }

func (g *geminiClient) messageCallIdentities(messages []message.Message) (map[string]string, map[string]string) {
	names, ids := map[string]string{}, map[string]string{}
	for _, msg := range messages {
		for _, call := range msg.ToolCalls() {
			names[call.ID] = call.Name
			ids[call.ID] = call.ID
		}
		g.restoreNativeCallIDs(msg, ids)
	}
	return names, ids
}
func (g *geminiClient) restoreNativeCallIDs(msg message.Message, ids map[string]string) {
	state := providerState(msg, g.providerOptions, "generate_content")
	if state == nil {
		return
	}
	var parts []geminiPart
	if json.Unmarshal(state, &parts) != nil {
		return
	}
	index := 0
	calls := msg.ToolCalls()
	for _, part := range parts {
		if part.FunctionCall == nil || index >= len(calls) {
			continue
		}
		ids[calls[index].ID] = part.FunctionCall.ID
		index++
	}
}
func (g *geminiClient) messageContent(msg message.Message, names, ids map[string]string) geminiContent {
	content := geminiContent{Role: "user"}
	switch msg.Role {
	case message.User:
		content.Parts = append(content.Parts, rawJSON(object{"text": msg.Content().String()}))
		for _, image := range msg.BinaryContent() {
			content.Parts = append(content.Parts, rawJSON(object{"inlineData": object{"mimeType": image.MIMEType, "data": image.Data}}))
		}
	case message.Assistant:
		content.Role = "model"
		content.Parts = g.assistantParts(msg)
	case message.Tool:
		for _, r := range msg.ToolResults() {
			content.Parts = append(content.Parts, geminiToolResult(r, names, ids))
		}
	}
	return content
}
func (g *geminiClient) assistantParts(msg message.Message) []json.RawMessage {
	var parts []json.RawMessage
	if state := providerState(msg, g.providerOptions, "generate_content"); state != nil {
		_ = json.Unmarshal(state, &parts)
	}
	if len(parts) > 0 {
		return parts
	}
	if text := msg.Content().String(); text != "" {
		parts = append(parts, rawJSON(object{"text": text}))
	}
	for _, call := range msg.ToolCalls() {
		if json.Valid([]byte(call.Input)) {
			parts = append(parts, rawJSON(object{"functionCall": object{"id": call.ID, "name": call.Name, "args": json.RawMessage(call.Input)}}))
		}
	}
	return parts
}
func geminiToolResult(r message.ToolResult, names, ids map[string]string) json.RawMessage {
	response := object{"result": r.Content}
	var parsed object
	if decodeObject([]byte(r.Content), &parsed) == nil && parsed != nil {
		response = parsed
	}
	if r.IsError {
		response = object{"error": r.Content}
	}
	functionResponse := object{"name": names[r.ToolCallID], "response": response}
	if id := ids[r.ToolCallID]; id != "" {
		functionResponse["id"] = id
	}
	return rawJSON(object{"functionResponse": functionResponse})
}

type geminiStreamState struct {
	ctx    context.Context
	events chan<- ProviderEvent
	final  geminiResponse
	parts  []json.RawMessage
	finish string
}

func (s *geminiStreamState) consume(_ string, raw json.RawMessage) (bool, error) {
	var chunk geminiResponse
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return false, err
	}
	if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
		return false, fmt.Errorf("provider Gemini stream error: %s", chunk.Error)
	}
	if chunk.Usage.Prompt != 0 || chunk.Usage.Output != 0 || chunk.Usage.Thoughts != 0 {
		s.final.Usage = chunk.Usage
	}
	for _, candidate := range chunk.Candidates {
		if candidate.Index != 0 {
			continue
		}
		if err := s.candidate(candidate); err != nil {
			return false, err
		}
	}
	return false, nil
}
func (s *geminiStreamState) candidate(candidate geminiCandidate) error {
	if candidate.FinishReason != "" && candidate.FinishReason != "FINISH_REASON_UNSPECIFIED" {
		s.finish = candidate.FinishReason
	}
	if candidate.Content == nil {
		return nil
	}
	for _, raw := range candidate.Content.Parts {
		s.parts = append(s.parts, raw)
		if err := s.part(raw); err != nil {
			return err
		}
	}
	return nil
}
func (s *geminiStreamState) part(raw json.RawMessage) error {
	var part geminiPart
	if err := json.Unmarshal(raw, &part); err != nil {
		return err
	}
	if part.Text == "" {
		return nil
	}
	event := ProviderEvent{Type: EventContentDelta, Content: part.Text}
	if part.Thought {
		event.Type = EventThinkingDelta
		event.Thinking = part.Text
	}
	if !publishEvent(s.ctx, s.events, event) {
		return s.ctx.Err()
	}
	return nil
}

func appendGeminiPart(result *ProviderResponse, rawPart json.RawMessage, finishReason string) error {

	var part geminiPart
	if err := json.Unmarshal(rawPart, &part); err != nil {
		return err
	}
	if !part.Thought {
		result.Content += part.Text
	}
	if call := part.FunctionCall; call != nil {
		if finishReason != "STOP" || call.Name == "" || !json.Valid(call.Args) {
			return fmt.Errorf("provider Gemini returned an incomplete function call")
		}
		id := call.ID
		if id == "" {
			id = "call_" + uuid.NewString()
		}
		result.ToolCalls = append(result.ToolCalls, message.ToolCall{ID: id, Name: call.Name, Input: string(call.Args), Type: "function", Finished: true})
	}
	return nil
}
