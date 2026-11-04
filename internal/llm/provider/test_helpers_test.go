package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

func (t *apiTransport) post(ctx context.Context, path string, payload any, stream bool) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode provider request: %w", err)
	}
	return t.postBytes(ctx, path, body, stream)
}

func (o *openaiClient) responseParams(messages []message.Message, tools []tools.BaseTool) object {
	return o.responsesRequest(messages, tools, false)
}

func WithCopilotBearerToken(token string) CopilotOption {
	return func(o *copilotOptions) { o.bearerToken = token }
}
