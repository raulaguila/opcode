package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

const probeName = "opcode_integration_probe"
const responseMarker = "OPCODE_OK"
const toolMarker = "OPCODE_TOOL_OK"

type probeTool struct{}

func (probeTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: probeName, Description: "A harmless integration probe that returns a fixed marker. It performs no I/O.", Parameters: map[string]any{"token": map[string]any{"type": "string", "enum": []string{"opcode_integration"}}}, Required: []string{"token"}}
}
func (probeTool) Run(_ context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	var args struct {
		Token string `json:"token"`
	}
	if json.Unmarshal([]byte(call.Input), &args) != nil || args.Token != "opcode_integration" {
		return tools.ToolResponse{}, errors.New("probe arguments do not match the contract")
	}
	return tools.NewTextResponse(toolMarker), nil
}
func prompt(text string) []message.Message {
	return []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}}
}

// Keep account data, request URLs, server bodies, model output and credential
// discovery diagnostics out of go test logs, including when errors echo a key.
func safeLiveError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline exceeded"
	case errors.Is(err, context.Canceled):
		return "request cancelled"
	}
	var httpErr *provider.HTTPError
	if errors.As(err, &httpErr) {
		return fmt.Sprintf("HTTP %d; check account access, model, endpoint and quota", httpErr.StatusCode)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network timeout"
	}
	return "provider request or protocol failed; details suppressed to protect credentials"
}
func checkText(t *testing.T, response *provider.ProviderResponse, marker string) {
	t.Helper()
	if response == nil || !strings.Contains(response.Content, marker) {
		t.Fatal("response did not contain the expected integration marker; body suppressed")
	}
	if response.FinishReason != message.FinishReasonEndTurn {
		t.Fatal("response did not finish normally; check model capabilities and maxTokens")
	}
	if len(response.ToolCalls) != 0 {
		t.Fatal("response unexpectedly requested a tool")
	}
}
func checkStream(t *testing.T, ctx context.Context, client provider.Provider, history []message.Message, definitions []tools.BaseTool) *provider.ProviderResponse {
	t.Helper()
	// A child context closes the HTTP response if validation exits early.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := client.StreamResponse(ctx, history, definitions)
	var complete *provider.ProviderResponse
	var content strings.Builder
	for {
		select {
		case <-ctx.Done():
			t.Fatal("stream did not finish before its deadline")
		case event, ok := <-events:
			if !ok {
				if complete == nil {
					t.Fatal("stream closed without a completion event")
				}
				if content.String() == "" || content.String() != complete.Content {
					t.Fatal("stream content deltas do not match the completed response; bodies suppressed")
				}
				return complete
			}
			switch event.Type {
			case provider.EventContentDelta:
				content.WriteString(event.Content)
			case provider.EventError:
				t.Fatal("stream failed:", safeLiveError(event.Error))
			case provider.EventComplete:
				if complete != nil || event.Response == nil {
					t.Fatal("invalid stream completion event")
				}
				complete = event.Response
			}
		}
	}
}
func runLiveChecks(t *testing.T, ctx context.Context, client provider.Provider, testTools bool) {
	t.Helper()
	if !t.Run("response", func(t *testing.T) {
		response, err := client.SendMessages(ctx, prompt("Reply with exactly "+responseMarker+". Do not call tools."), nil)
		if err != nil {
			t.Fatal("request failed:", safeLiveError(err))
		}
		checkText(t, response, responseMarker)
	}) {
		return
	}
	if !t.Run("stream", func(t *testing.T) {
		checkText(t, checkStream(t, ctx, client, prompt("Reply with exactly "+responseMarker+". Do not call tools."), nil), responseMarker)
	}) {
		return
	}
	t.Run("tool_round_trip", func(t *testing.T) {
		if !testTools {
			t.Skip("tool test explicitly disabled in integration configuration")
		}
		definition := probeTool{}
		definitions := []tools.BaseTool{definition}
		history := prompt("Call " + probeName + " exactly once with token=opcode_integration. Do not answer before receiving the tool result. After receiving it, reply with exactly " + toolMarker + " and do not call any tool again.")
		response, err := client.SendMessages(ctx, history, definitions)
		if err != nil {
			t.Fatal("tool request failed:", safeLiveError(err))
		}
		if response == nil || response.FinishReason != message.FinishReasonToolUse || len(response.ToolCalls) != 1 {
			t.Fatal("model did not return exactly one probe call; check model tool support and maxTokens")
		}
		call := response.ToolCalls[0]
		if call.Name != probeName || call.ID == "" || !call.Finished {
			t.Fatal("probe call has an invalid name, identifier or completion state; values suppressed")
		}
		result, err := definition.Run(ctx, tools.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input})
		if err != nil {
			t.Fatal("probe arguments did not match the test contract; values suppressed")
		}
		// Reproduce the agent's persisted history, including opaque reasoning state.
		assistant := message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: response.Content}, call}}
		assistant.AddFinish(response.FinishReason)
		assistant.SetProviderState(response.ProviderState)
		history = append(history, assistant, message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: result.Content}}})
		checkText(t, checkStream(t, ctx, client, history, definitions), toolMarker)
	})
}
