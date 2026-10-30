// Package diagnostic contains opt-in, bounded checks that never execute tools.
package diagnostic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

const ProbeTimeout = 45 * time.Second

// ProbeError contains only a controlled category/status, never a provider body,
// URL, credential or response. Callers may safely display Error().
type ProbeError struct {
	Kind       string
	StatusCode int
}

func (e *ProbeError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("model tool probe failed: %s (HTTP %d)", e.Kind, e.StatusCode)
	}
	return "model tool probe failed: " + e.Kind
}

// ProbeModel sends one logical generation request after explicit caller/user
// opt-in. connection must be a resolved snapshot; model may be an unsaved catalog
// candidate. It never changes config, model registries, sessions or permissions.
// The private copy uses at most 4096 context tokens and 256 generated tokens.
// Native transport retries remain bounded by the caller deadline and 45s cap.
// A successful challenge is capability evidence for this request, not a quality
// benchmark. The model's requested tool is inspected and never executed.
func ProbeModel(ctx context.Context, model models.Model, connection config.Provider) error {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if connection.Disabled || model.Provider == "" || model.APIModel == "" {
		return &ProbeError{Kind: "invalid model or disabled connection"}
	}
	if model.API == "" {
		model.API = connection.API
	}
	if model.ContextWindow <= 0 {
		model.ContextWindow = 4096
	}
	model.ContextWindow = min(model.ContextWindow, 4096)
	model.DefaultMaxTokens = min(256, max(1, model.ContextWindow/4))
	// The passed connection is authoritative, including for an unsaved local
	// candidate. Do not let LOCAL_ENDPOINT replace this connection snapshot.
	if model.Provider == models.ProviderLocal && model.ProviderProfile == "" {
		model.ProviderProfile = "__diagnostic_snapshot"
	}
	var challenge [16]byte
	if _, err := rand.Read(challenge[:]); err != nil {
		return &ProbeError{Kind: "challenge generation unavailable"}
	}
	nonce := hex.EncodeToString(challenge[:])
	p, err := provider.NewProvider(model.Provider, provider.WithAPIKey(connection.APIKey), provider.WithBaseURL(connection.BaseURL), provider.WithModel(model), provider.WithMaxTokens(model.DefaultMaxTokens), provider.WithSystemMessage("You are performing a structured tool capability check. Call the provided tool exactly once. Do not produce prose."))
	if err != nil {
		return &ProbeError{Kind: "connection setup"}
	}
	response, err := p.SendMessages(ctx, []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Call opcode_health_check with challenge " + nonce}}}}, []tools.BaseTool{probeTool{}})
	if err != nil {
		return probeRequestError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if response == nil || len(response.ToolCalls) != 1 {
		return &ProbeError{Kind: "structured tool call not verified"}
	}
	call := response.ToolCalls[0]
	var args struct{ Challenge string }
	if call.Name != "opcode_health_check" || json.Unmarshal([]byte(call.Input), &args) != nil || args.Challenge != nonce {
		return &ProbeError{Kind: "structured tool call not verified"}
	}
	return nil
}

type probeTool struct{}

func (probeTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: "opcode_health_check", Description: "Inert capability probe. Return the exact challenge requested by the user.", Parameters: map[string]any{"challenge": map[string]any{"type": "string"}}, Required: []string{"challenge"}}
}
func (probeTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	return tools.ToolResponse{}, errors.New("diagnostic probe tools must never execute")
}

func probeRequestError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if httpError, ok := errors.AsType[*provider.HTTPError](err); ok {
		kind := "provider response"
		switch {
		case httpError.StatusCode == 401 || httpError.StatusCode == 403:
			kind = "authentication"
		case httpError.StatusCode == 429:
			kind = "rate limit"
		case httpError.StatusCode >= 500:
			kind = "provider unavailable"
		}
		return &ProbeError{Kind: kind, StatusCode: httpError.StatusCode}
	}
	return &ProbeError{Kind: "request failed"}
}
