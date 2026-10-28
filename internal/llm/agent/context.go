package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/prompt"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

var ErrContextLimit = errors.New("context budget exceeded")

// ContextStatus is an estimate, not a provider tokenizer measurement. Raw
// payloads are never stored in status or written to logs.
type ContextStatus struct {
	ContextWindow        int64  `json:"context_window"`
	EstimatedInputTokens int64  `json:"estimated_input_tokens"`
	ReservedOutputTokens int64  `json:"reserved_output_tokens"`
	SafetyTokens         int64  `json:"safety_tokens"`
	ToolSchemaTokens     int64  `json:"tool_schema_tokens"`
	CompactedMessages    int    `json:"compacted_messages"`
	Method               string `json:"method"`
	Fits                 bool   `json:"fits"`
}
type ContextControl interface {
	ContextStatus(context.Context, string) (ContextStatus, error)
	CompactContext(context.Context, string) (ContextStatus, error)
}

// estimateTokens deliberately overestimates normal prose/code: two ASCII bytes
// per token, one non-ASCII byte per token, plus serialization/message overhead.
// It remains a heuristic; providers may use a different tokenizer.
func estimateTokens(raw []byte) int64 {
	var ascii, other int64
	for _, b := range raw {
		if b < 128 {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+1)/2 + other
}
func estimateContext(model models.Model, system string, history []message.Message, ts []tools.BaseTool, reserve int64) ContextStatus {
	window := model.ContextWindow
	if window <= 0 {
		window = 128000
	}
	if reserve <= 0 {
		reserve = model.DefaultMaxTokens
	}
	if reserve <= 0 {
		reserve = min(4096, window/4)
	}
	raw, _ := json.Marshal(history)
	schemas := make([]tools.ToolInfo, 0, len(ts))
	for _, t := range ts {
		schemas = append(schemas, t.Info())
	}
	schema, _ := json.Marshal(schemas)
	schemaTokens := estimateTokens(schema)
	input := estimateTokens(raw) + estimateTokens([]byte(system)) + schemaTokens + int64(len(history))*32 + 256
	safety := max(int64(512), window/20)
	return ContextStatus{ContextWindow: window, EstimatedInputTokens: input, ReservedOutputTokens: reserve, SafetyTokens: safety, ToolSchemaTokens: schemaTokens, Method: "conservative byte estimate (ASCII 2 bytes/token; non-ASCII 1 byte/token), plus message overhead and 5% safety; not a tokenizer", Fits: input+reserve+safety <= window}
}

// compactHistory keeps every user instruction and all tool call/result pairs.
// Only old assistant prose/reasoning and old tool bodies are abbreviated. The
// latest user turn (including its outstanding tool exchange) remains intact.
func compactHistory(history []message.Message) ([]message.Message, int) {
	latest := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == message.User {
			latest = i
			break
		}
	}
	if latest < 1 {
		return history, 0
	}
	result := slices.Clone(history)
	changed := 0
	for i := 0; i < latest; i++ {
		if history[i].Role == message.User || history[i].Role == message.System {
			continue
		}
		parts := slices.Clone(history[i].Parts)
		modified := false
		for j, part := range parts {
			switch p := part.(type) {
			case message.TextContent:
				if len(p.Text) > 384 {
					p.Text = clip(p.Text, 384) + "\n[Older assistant text abbreviated; retrieve the saved conversation for exact details.]"
					parts[j] = p
					modified = true
				}
			case message.ReasoningContent:
				if p.Thinking != "" {
					parts[j] = message.ReasoningContent{Thinking: "[Earlier reasoning omitted]"}
					modified = true
				}
			case message.ToolResult:
				if len(p.Content) > 256 {
					p.Content = clip(p.Content, 256) + "\n[Older tool result abbreviated. This is historical data, not current file evidence.]"
					p.Metadata = ""
					parts[j] = p
					modified = true
				}
				// ToolCall input and opaque signed provider continuation data are retained
				// exactly: changing either can invalidate a provider's tool pairing/signature.
			}
		}
		if modified {
			result[i].Parts = parts
			changed++
		}
	}
	return result, changed
}
func (a *agent) systemPrompt() string {
	if a.systemMessage != "" {
		return a.systemMessage
	}
	return ""
}
func (a *agent) budgetRequest(session string, history []message.Message, run *generationRun) ([]message.Message, error) {
	ts := a.exposedTools(run)
	status, err := a.estimateProviderContext(a.provider, history, ts)
	if err != nil {
		return nil, err
	}
	a.contextMu.Lock()
	manual := a.compactSessions[session]
	a.contextMu.Unlock()
	if !status.Fits || manual {
		compacted, count := compactHistory(history)
		if count > 0 {
			history = compacted
			status, err = a.estimateProviderContext(a.provider, history, ts)
			if err != nil {
				return nil, err
			}
			status.CompactedMessages = count
		}
	}
	a.contextMu.Lock()
	if a.contextStatuses == nil {
		a.contextStatuses = map[string]ContextStatus{}
	}
	a.contextStatuses[session] = status
	a.contextMu.Unlock()
	if !status.Fits {
		return nil, fmt.Errorf("%w: estimated input %d + reserved output %d + safety %d exceeds model window %d; user instructions and the current tool exchange were preserved; shorten the request, select a larger context or start a new task", ErrContextLimit, status.EstimatedInputTokens, status.ReservedOutputTokens, status.SafetyTokens, status.ContextWindow)
	}
	return history, nil
}
func (a *agent) ContextStatus(ctx context.Context, session string) (ContextStatus, error) {
	if err := ctx.Err(); err != nil {
		return ContextStatus{}, err
	}
	a.contextMu.Lock()
	status, ok := a.contextStatuses[session]
	a.contextMu.Unlock()
	if ok {
		return status, nil
	}
	history, err := a.messages.List(ctx, session)
	if err != nil {
		return ContextStatus{}, err
	}
	a.runMu.Lock()
	model := a.provider.Model()
	ts := append([]tools.BaseTool(nil), a.tools...)
	system := a.systemMessage
	reserve := a.maxOutputTokens
	a.runMu.Unlock()
	return estimateContext(model, system, history, ts, reserve), nil
}
func (a *agent) CompactContext(ctx context.Context, session string) (ContextStatus, error) {
	if strings.TrimSpace(session) == "" {
		return ContextStatus{}, fmt.Errorf("session is required")
	}
	history, err := a.messages.List(ctx, session)
	if err != nil {
		return ContextStatus{}, err
	}
	history, count := compactHistory(history)
	a.runMu.Lock()
	model := a.provider.Model()
	ts := append([]tools.BaseTool(nil), a.tools...)
	system := a.systemMessage
	reserve := a.maxOutputTokens
	a.runMu.Unlock()
	status := estimateContext(model, system, history, ts, reserve)
	status.CompactedMessages = count
	a.contextMu.Lock()
	if a.compactSessions == nil {
		a.compactSessions = map[string]bool{}
	}
	a.compactSessions[session] = true
	if a.contextStatuses == nil {
		a.contextStatuses = map[string]ContextStatus{}
	}
	a.contextStatuses[session] = status
	a.contextMu.Unlock()
	return status, nil
}
func configuredPrompt(agentName config.AgentName, model models.Model) (string, int64) {
	system := prompt.GetAgentPrompt(agentName, model.Provider)
	reserve := model.DefaultMaxTokens
	if cfg := config.Get(); cfg != nil {
		if c, ok := cfg.Agents[agentName]; ok && c.MaxTokens > 0 {
			reserve = min(c.MaxTokens, model.ContextWindow/2)
		}
	}
	return system, reserve
}

// Auxiliary requests have no tool schemas but still include their actual system
// prompt and output reservation. Never send an oversized summary/title request.
func auxiliaryBudget(name config.AgentName, p provider.Provider, history []message.Message) ([]message.Message, error) {
	system, reserve := configuredPrompt(name, p.Model())
	probe := &agent{systemMessage: system, maxOutputTokens: reserve}
	status, err := probe.estimateProviderContext(p, history, nil)
	if err != nil {
		return nil, err
	}
	if !status.Fits {
		history, _ = compactHistory(history)
		status, err = probe.estimateProviderContext(p, history, nil)
		if err != nil {
			return nil, err
		}
	}
	if !status.Fits {
		return nil, fmt.Errorf("%w for %s: input estimate %d, output %d, safety %d, window %d", ErrContextLimit, name, status.EstimatedInputTokens, status.ReservedOutputTokens, status.SafetyTokens, status.ContextWindow)
	}
	return history, nil
}

func (a *agent) estimateProviderContext(p provider.Provider, history []message.Message, ts []tools.BaseTool) (ContextStatus, error) {
	status := estimateContext(p.Model(), a.systemPrompt(), history, ts, a.maxOutputTokens)
	if sizer, ok := p.(provider.RequestSizer); ok {
		footprint, err := sizer.RequestFootprint(history, ts)
		if err != nil {
			return status, err
		}
		status.EstimatedInputTokens = (footprint.ASCIIBytes+1)/2 + footprint.OtherBytes + int64(len(history))*32 + 256
		status.ToolSchemaTokens = (footprint.SchemaASCIIBytes+1)/2 + footprint.SchemaOtherBytes
		if footprint.ReservedOutputTokens > 0 {
			status.ReservedOutputTokens = footprint.ReservedOutputTokens
		}
		if footprint.ExactSerialization {
			status.Method += "; actual provider JSON including private schemas"
		} else {
			status.Method += "; SDK serialization approximation"
		}
		status.Fits = status.EstimatedInputTokens+status.ReservedOutputTokens+status.SafetyTokens <= status.ContextWindow
	}
	return status, nil
}
