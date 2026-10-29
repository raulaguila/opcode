package provider

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type ollamaClient struct {
	*httpProvider
	opts providerClientOptions
}

const ollamaFinalAnswer = "final_answer"
const ollamaSourceAnswer = "answer_with_sources"

func newOllamaClient(opts providerClientOptions, endpoint string) *ollamaClient {
	c := &ollamaClient{opts: opts}
	transport := newAPITransport(strings.TrimSuffix(endpoint, "/v1"))
	if opts.apiKey != "" {
		transport.headers.Set("Authorization", "Bearer "+opts.apiKey)
	}
	c.httpProvider = &httpProvider{transport: transport, protocol: c}
	return c
}

func (o *ollamaClient) convertMessages(history []message.Message) ([]object, error) {
	result := []object{{"role": "system", "content": o.opts.systemMessage}}
	names := map[string]string{}
	for _, msg := range history {
		item := object{"role": msg.Role, "content": msg.Content().String()}
		switch msg.Role {
		case message.User:
			var images []string
			for _, image := range msg.BinaryContent() {
				images = append(images, image.String(models.ProviderLocal))
			}
			if len(images) > 0 {
				item["images"] = images
			}
		case message.Assistant:
			if thinking := msg.ReasoningContent().String(); thinking != "" {
				item["thinking"] = thinking
			}
			var calls []object
			for _, call := range msg.ToolCalls() {
				if !ollamaArgumentsValid(json.RawMessage(call.Input)) {
					return nil, fmt.Errorf("invalid Ollama tool arguments in history")
				}
				names[call.ID] = call.Name
				calls = append(calls, object{"id": call.ID, "type": "function", "function": object{"name": call.Name, "arguments": json.RawMessage(call.Input)}})
			}
			if len(calls) > 0 {
				item["tool_calls"] = calls
			}
		case message.Tool:
			for _, value := range msg.ToolResults() {
				result = append(result, object{"role": "tool", "tool_call_id": value.ToolCallID, "tool_name": cmp.Or(value.Name, names[value.ToolCallID]), "content": value.Content})
			}
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func (o *ollamaClient) request(history []message.Message, definitions []tools.BaseTool, stream bool) (string, any, error) {
	messages, err := o.convertMessages(history)
	if err != nil {
		return "", nil, err
	}
	params := object{"model": o.opts.model.APIModel, "messages": messages, "stream": stream, "truncate": false,
		"options": object{"num_ctx": cmp.Or(o.opts.model.ContextWindow, 16384), "num_predict": cmp.Or(o.opts.maxTokens, 2048), "temperature": 0}}
	if o.opts.model.CanReason {
		params["think"] = true
	}
	var functions []object
	for _, tool := range definitions {
		info := tool.Info()
		if info.Name == ollamaFinalAnswer || info.Name == ollamaSourceAnswer {
			return "", nil, fmt.Errorf("Ollama tool name final_answer is reserved for replies")
		}
		functions = append(functions, object{"type": "function", "function": object{"name": info.Name, "description": info.Description, "parameters": toolSchema(tool)}})
	}
	if len(functions) > 0 {
		// Some Ollama templates request a function for every trailing user
		// message. Give them a way to answer without inventing a shell action.
		final := object{
			"name": ollamaFinalAnswer, "description": "Reply to the user with your final answer after gathering needed facts or completing the requested work. This only sends your answer; it does not execute a command. Use this when no further tool operation is needed.",
			"parameters": object{"type": "object", "properties": object{"answer": object{"type": "string", "description": "The complete answer to the original user request in its language, including the requested facts and inspected path:line citations for code explanations."}}, "required": []string{"answer"}},
		}
		instruction := "\nWhen the required facts or work are complete, call final_answer with your answer text to finish the turn. A request to reply or acknowledge something uses final_answer, never bash echo/printf or another operation."
		if len(o.opts.sourceCitations) > 0 {
			final = object{"name": ollamaSourceAnswer, "description": "Finish the corrected explanation of the original user's project or code defect. This sends a reply, never executes an operation. Explain the actual finding in the user's language and select the inspected source.",
				"parameters": object{"type": "object", "additionalProperties": false, "properties": object{
					"source":      object{"type": "string", "enum": o.opts.sourceCitations, "description": "Select one inspected file and line interval."},
					"explanation": object{"type": "string", "description": "Answer the ORIGINAL user request in its language. Preserve the project name and requested facts or explain the actual defect. Do not discuss the application validation notice."},
				}, "required": []string{"source", "explanation"}}}
			instruction = "\nFor this citation correction, finish with answer_with_sources: choose source from its enum and put your corrected answer in explanation. Do not call final_answer for this correction. Do not explain the validation notice or repeat successful reads. Source selection is still verified by the application."
		}
		functions = append(functions, object{"type": "function", "function": final})
		messages[0]["content"] = o.opts.systemMessage + instruction
		params["tools"] = functions
	}
	return "/api/chat", params, nil
}

type ollamaCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaResponse struct {
	Message struct {
		Content   string       `json:"content"`
		Thinking  string       `json:"thinking"`
		ToolCalls []ollamaCall `json:"tool_calls"`
	} `json:"message"`
	Done            bool            `json:"done"`
	DoneReason      string          `json:"done_reason"`
	PromptEvalCount int64           `json:"prompt_eval_count"`
	EvalCount       int64           `json:"eval_count"`
	Error           json.RawMessage `json:"error"`
}

func ollamaArgumentsValid(raw json.RawMessage) bool {
	text := strings.TrimSpace(string(raw))
	return strings.HasPrefix(text, "{") && json.Valid(raw)
}

func ollamaResult(value ollamaResponse) (*ProviderResponse, error) {
	if len(value.Error) > 0 && string(value.Error) != "null" {
		return nil, ollamaFailure(value.Error)
	}
	if !value.Done {
		return nil, fmt.Errorf("Ollama response ended without done=true")
	}
	response := &ProviderResponse{Content: value.Message.Content, FinishReason: message.FinishReasonEndTurn,
		Usage: TokenUsage{InputTokens: value.PromptEvalCount, OutputTokens: value.EvalCount}}
	if value.DoneReason == "length" {
		response.FinishReason = message.FinishReasonMaxTokens
	} else if value.DoneReason != "" && value.DoneReason != "stop" {
		return nil, fmt.Errorf("Ollama returned an unexpected completion reason")
	}
	finalAnswer := ""
	for _, call := range value.Message.ToolCalls {
		if response.FinishReason == message.FinishReasonMaxTokens || call.Function.Name == "" || !ollamaArgumentsValid(call.Function.Arguments) {
			if response.FinishReason != message.FinishReasonMaxTokens && (call.Function.Name == ollamaFinalAnswer || call.Function.Name == ollamaSourceAnswer) {
				return nil, fmt.Errorf("%w: incomplete Ollama reply arguments", ErrInvalidFinalAnswer)
			}
			return nil, fmt.Errorf("Ollama returned an incomplete tool call")
		}
		if call.Function.Name == ollamaFinalAnswer || call.Function.Name == ollamaSourceAnswer {
			var args struct {
				Answer      string `json:"answer"`
				Citations   string `json:"citations_text"`
				Source      string `json:"source"`
				Explanation string `json:"explanation"`
			}
			if finalAnswer != "" || json.Unmarshal(call.Function.Arguments, &args) != nil {
				return nil, fmt.Errorf("%w: invalid Ollama final answer", ErrInvalidFinalAnswer)
			}
			if call.Function.Name == ollamaSourceAnswer {
				if strings.TrimSpace(args.Source) == "" || strings.TrimSpace(args.Explanation) == "" {
					return nil, fmt.Errorf("%w: invalid Ollama source answer", ErrInvalidFinalAnswer)
				}
				args.Answer, args.Citations = args.Explanation, args.Source
			}
			if strings.TrimSpace(args.Answer) == "" {
				return nil, fmt.Errorf("%w: invalid Ollama final answer", ErrInvalidFinalAnswer)
			}
			finalAnswer = args.Answer
			if len(args.Citations) > 2048 {
				return nil, fmt.Errorf("%w: Ollama final answer citations exceed limit", ErrInvalidFinalAnswer)
			}
			if strings.TrimSpace(args.Citations) != "" {
				// These are model-provided references, not application assertions.
				// The agent still verifies every path/range/hash before completion.
				finalAnswer += "\n\n" + strings.TrimSpace(args.Citations)
			}
			continue
		}
		response.ToolCalls = append(response.ToolCalls, message.ToolCall{ID: cmp.Or(call.ID, uuid.NewString()), Name: call.Function.Name,
			Input: string(call.Function.Arguments), Type: "function", Finished: true})
	}
	if len(response.ToolCalls) > 0 {
		// A premature answer cannot conclude a batch of operations whose results
		// the model has not seen. Discard it; the ordinary agent loop executes
		// the real calls under its existing mode/approval checks, then asks again.
		response.FinishReason = message.FinishReasonToolUse
	} else {
		response.Content += finalAnswer
	}
	return response, nil
}

func (o *ollamaClient) decodeResponse(raw json.RawMessage) (*ProviderResponse, error) {
	var value ollamaResponse
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return ollamaResult(value)
}

func (o *ollamaClient) readStream(ctx context.Context, reader io.Reader, events chan<- ProviderEvent) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxProviderBytes)
	var content strings.Builder
	var calls []ollamaCall
	total := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw := scanner.Bytes()
		total += len(raw)
		if total > maxProviderBytes {
			return fmt.Errorf("Ollama stream exceeds maximum size")
		}
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var value ollamaResponse
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("invalid Ollama stream JSON")
		}
		if len(value.Error) > 0 && string(value.Error) != "null" {
			return ollamaFailure(value.Error)
		}
		content.WriteString(value.Message.Content)
		calls = append(calls, value.Message.ToolCalls...)
		for _, event := range []ProviderEvent{{Type: EventContentDelta, Content: value.Message.Content}, {Type: EventThinkingDelta, Content: value.Message.Thinking, Thinking: value.Message.Thinking}} {
			if event.Content != "" && !publishEvent(ctx, events, event) {
				return ctx.Err()
			}
		}
		if value.Done {
			value.Message.Content = content.String()
			value.Message.ToolCalls = calls
			response, err := ollamaResult(value)
			if err != nil {
				return err
			}
			if answer := strings.TrimPrefix(response.Content, value.Message.Content); answer != "" {
				if !publishEvent(ctx, events, ProviderEvent{Type: EventContentDelta, Content: answer}) {
					return ctx.Err()
				}
			}
			if !publishEvent(ctx, events, ProviderEvent{Type: EventComplete, Response: response}) {
				return ctx.Err()
			}
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("Ollama stream ended without done=true")
}

// Retain a safe classification without exposing the provider's raw error body.
func ollamaFailure(raw json.RawMessage) error {
	text := strings.ToLower(string(raw))
	for _, term := range []string{"out of memory", "requires more system memory", "unable to allocate"} {
		if strings.Contains(text, term) {
			return fmt.Errorf("Ollama out of memory; reduce context or select a smaller model")
		}
	}
	for _, term := range []string{"context length", "context window", "too many tokens"} {
		if strings.Contains(text, term) {
			return fmt.Errorf("Ollama context window exceeded")
		}
	}
	return fmt.Errorf("Ollama returned an error")
}
