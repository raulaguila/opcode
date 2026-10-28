package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

func (o *openaiClient) responseParams(messages []message.Message, tools []tools.BaseTool) object {
	return o.responsesRequest(messages, tools, false)
}
func (o *openaiClient) responsesRequest(messages []message.Message, tools []tools.BaseTool, stream bool) object {
	input := []any{}
	for _, msg := range messages {
		switch msg.Role {
		case message.User:
			content := []object{{"type": "input_text", "text": msg.Content().String()}}
			for _, image := range msg.BinaryContent() {
				content = append(content, object{"type": "input_image", "image_url": image.String(models.ProviderOpenAI), "detail": "auto"})
			}
			input = append(input, object{"role": "user", "content": content})
		case message.Assistant:
			if state := providerState(msg, o.providerOptions, "responses"); state != nil {
				var items []json.RawMessage
				if json.Unmarshal(state, &items) == nil && len(items) > 0 {
					for _, item := range items {
						input = append(input, item)
					}
					continue
				}
			}
			if text := msg.Content().String(); text != "" {
				input = append(input, object{"role": "assistant", "content": []object{{"type": "output_text", "text": text}}})
			}
			for _, call := range msg.ToolCalls() {
				input = append(input, object{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": call.Input})
			}
		case message.Tool:
			for _, result := range msg.ToolResults() {
				input = append(input, object{"type": "function_call_output", "call_id": result.ToolCallID, "output": result.Content})
			}
		}
	}
	params := object{"model": o.providerOptions.model.APIModel, "instructions": o.providerOptions.systemMessage, "input": input, "max_output_tokens": o.providerOptions.maxTokens, "store": false, "include": []string{"reasoning.encrypted_content"}, "stream": stream}
	if o.providerOptions.model.CanReason && o.options.reasoningEffort != "" {
		params["reasoning"] = object{"effort": o.options.reasoningEffort}
	}
	var definitions []object
	for _, tool := range tools {
		info := tool.Info()
		definitions = append(definitions, object{"type": "function", "name": info.Name, "description": info.Description, "strict": false, "parameters": toolSchema(tool)})
	}
	if len(definitions) > 0 {
		params["tools"] = definitions
	}
	return params
}

type responsesResponse struct {
	Status string            `json:"status"`
	Output []json.RawMessage `json:"output"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage struct {
		Input   int64 `json:"input_tokens"`
		Output  int64 `json:"output_tokens"`
		Details struct {
			Cached int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

func (o *openaiClient) decodeResponses(raw json.RawMessage) (*ProviderResponse, error) {
	var r responsesResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.Error != nil {
		return nil, fmt.Errorf("Responses API failed: %s", r.Error.Message)
	}
	if r.Status != "completed" && r.Status != "incomplete" {
		return nil, fmt.Errorf("Responses API returned nonterminal status %q", r.Status)
	}
	if len(r.Output) == 0 {
		return nil, fmt.Errorf("Responses API returned no output")
	}
	result := &ProviderResponse{FinishReason: message.FinishReasonEndTurn}
	for _, rawItem := range r.Output {
		var item struct {
			Type, Status, Name, Arguments string
			CallID                        string                                 `json:"call_id"`
			Content                       []struct{ Type, Text, Refusal string } `json:"content"`
		}
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return nil, err
		}
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					result.Content += part.Text
				}
				if part.Type == "refusal" {
					result.Content += part.Refusal
				}
			}
		case "function_call":
			if r.Status != "completed" || item.Status != "" && item.Status != "completed" || item.CallID == "" || item.Name == "" || !json.Valid([]byte(item.Arguments)) {
				return nil, fmt.Errorf("Responses API returned an incomplete function call")
			}
			result.ToolCalls = append(result.ToolCalls, message.ToolCall{ID: item.CallID, Name: item.Name, Input: item.Arguments, Type: "function", Finished: true})
		}
	}
	result.ProviderState = newProviderState(o.providerOptions, "responses", r.Output)
	if len(result.ToolCalls) > 0 {
		result.FinishReason = message.FinishReasonToolUse
	} else if r.Status == "incomplete" {
		result.FinishReason = message.FinishReasonMaxTokens
	}
	result.Usage = TokenUsage{InputTokens: max(0, r.Usage.Input-r.Usage.Details.Cached), OutputTokens: r.Usage.Output, CacheReadTokens: r.Usage.Details.Cached}
	return result, nil
}

func (o *openaiClient) readResponsesStream(ctx context.Context, r io.Reader, events chan<- ProviderEvent) error {
	complete := false
	err := readSSE(r, func(_ string, raw json.RawMessage) (bool, error) {
		var event struct {
			Type, Delta, Message string
			Response             json.RawMessage
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return false, err
		}
		switch event.Type {
		case "response.output_text.delta", "response.refusal.delta", "response.reasoning_summary_text.delta":
			output := ProviderEvent{Type: EventContentDelta, Content: event.Delta}
			if event.Type == "response.reasoning_summary_text.delta" {
				output.Type = EventThinkingDelta
				output.Thinking = event.Delta
			}
			if !publishEvent(ctx, events, output) {
				return false, ctx.Err()
			}
		case "response.completed", "response.incomplete":
			result, err := o.decodeResponses(event.Response)
			if err != nil {
				return false, err
			}
			if !publishEvent(ctx, events, ProviderEvent{Type: EventComplete, Response: result}) {
				return false, ctx.Err()
			}
			complete = true
			return true, nil
		case "response.failed", "error":
			return false, fmt.Errorf("Responses API stream failed: %s %s", event.Message, event.Response)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if !complete {
		return fmt.Errorf("Responses API stream ended without a terminal event")
	}
	return nil
}
