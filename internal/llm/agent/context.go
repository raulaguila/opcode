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
	"github.com/raulaguila/opcode/internal/workflow"
)

var ErrContextLimit = errors.New("context budget exceeded")

// ContextStatus is an estimate, not a provider tokenizer measurement. Raw
// payloads are never stored in status or written to logs.
type ContextStatus struct {
	measuredForRequest   bool
	uncalibratedInput    int64
	ContextSource        string           `json:"context_source"`
	ActualInputTokens    int64            `json:"actual_input_tokens,omitempty"`
	CalibrationSamples   int              `json:"calibration_samples"`
	ComponentBudgets     map[string]int64 `json:"component_budgets"`
	Components           map[string]int64 `json:"components"`
	ContextWindow        int64            `json:"context_window"`
	EstimatedInputTokens int64            `json:"estimated_input_tokens"`
	ReservedOutputTokens int64            `json:"reserved_output_tokens"`
	SafetyTokens         int64            `json:"safety_tokens"`
	ToolSchemaTokens     int64            `json:"tool_schema_tokens"`
	CompactedMessages    int              `json:"compacted_messages"`
	Method               string           `json:"method"`
	Fits                 bool             `json:"fits"`
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
	return ContextStatus{ContextSource: model.ContextSource, Components: map[string]int64{"instructions": estimateTokens([]byte(system)), "history_and_evidence": estimateTokens(raw), "schemas": schemaTokens, "output": reserve, "safety": safety}, ContextWindow: window, EstimatedInputTokens: input, ReservedOutputTokens: reserve, SafetyTokens: safety, ToolSchemaTokens: schemaTokens, Method: "conservative byte estimate (ASCII 2 bytes/token; non-ASCII 1 byte/token), plus message overhead and 5% safety; not a tokenizer", Fits: input+reserve+safety <= window}
}

// compactHistory keeps every user instruction and all tool call/result pairs.
// Only old assistant prose/reasoning and old tool bodies are abbreviated. The
// latest user turn (including its outstanding tool exchange) remains intact.
func compactHistory(history []message.Message) ([]message.Message, int) {
	latest := -1
	for i, h := range slices.Backward(history) {
		if h.Role == message.User {
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
		if next, modified := compactHistoricalMessage(history[i]); modified {
			result[i] = next
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
	bounded, _, err := a.budgetPreparedRequest(session, history, run)
	return bounded, err
}

func (a *agent) budgetPreparedRequest(session string, history []message.Message, run *generationRun) ([]message.Message, provider.PreparedRequest, error) {
	p := runProvider(a, run)
	preparation := &requestPreparation{Provider: p}
	if preparer, ok := p.(provider.RequestPreparer); ok {
		preparation.preparer = preparer
		p = preparation
	}
	bounded, err := a.budgetRequestWithProvider(session, history, run, p)
	if err != nil {
		return nil, nil, err
	}
	return bounded, preparation.prepared, nil
}

func (a *agent) budgetRequestWithProvider(session string, history []message.Message, run *generationRun, p provider.Provider) ([]message.Message, error) {
	ts := a.exposedTools(run)
	status, err := a.estimateProviderContext(p, history, ts)
	if err != nil {
		return nil, err
	}
	a.contextMu.Lock()
	manual := a.compactSessions[session]
	a.contextMu.Unlock()
	if !status.Fits || manual || status.EstimatedInputTokens+status.ReservedOutputTokens+status.SafetyTokens > status.ContextWindow*85/100 {
		history, status, err = a.compactRequestHistory(p, history, status, ts, compactHistory)
		if err != nil {
			return nil, err
		}
	}
	// Initial progressive reads are one bootstrap exchange: let the model see
	// them before proactively replacing closed batches with historical locators.
	// The hard serialized budget (including output and safety) always applies.
	if !status.Fits || manual || (run.steps > 1 && status.EstimatedInputTokens+status.ReservedOutputTokens+status.SafetyTokens > status.ContextWindow*85/100) {
		history, status, err = a.compactRequestHistory(p, history, status, ts, compactCompletedBatches)
		if err != nil {
			return nil, err
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
	return a.previewContext(ctx, session, history)
}
func (a *agent) previewContext(ctx context.Context, session string, history []message.Message) (ContextStatus, error) {
	a.runMu.Lock()
	p := a.provider
	available := append([]tools.BaseTool(nil), a.tools...)
	a.runMu.Unlock()
	preview := &generationRun{ctx: ctx, provider: p, mode: workflow.Execute, available: available}
	if a.workflow != nil {
		if session != "" {
			task, e := a.workflow.Get(ctx, session)
			if e != nil {
				return ContextStatus{}, e
			}
			preview.mode = task.EffectiveMode()
		}
		preview.available = append(preview.available, a.contextTools(preview, session)...)
		preview.available = append(preview.available, a.workflowTools(preview, session)...)
		selectInitialSchemas(preview)
	} else {
		preview.selected = map[string]bool{}
		for _, tool := range available {
			preview.selected[tool.Info().Name] = true
		}
	}
	p = learnedContextProvider(p)
	preview.provider = p
	selected := a.exposedTools(preview)
	if controller, ok := p.(provider.SystemPromptController); ok {
		names := []string{}
		for _, tool := range selected {
			names = append(names, tool.Info().Name)
		}
		model := p.Model()
		next, e := controller.WithSystemPrompt(prompt.RunPrompt(string(preview.mode), model.API, model.ContextWindow, names))
		if e != nil {
			return ContextStatus{}, e
		}
		p = next
	}
	status, e := a.estimateProviderContext(p, history, selected)
	status.Method += "; preview of initial selected schemas (next request adds current task/input)"
	return status, e

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
	history, batches := compactCompletedBatches(history)
	count += batches
	status, err := a.previewContext(ctx, session, history)
	if err != nil {
		return ContextStatus{}, err
	}
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

// Auxiliary requests include their actual system prompt, any exploration tool
// schemas and output reservation. Never send an oversized auxiliary request.
func auxiliaryBudgetTools(name config.AgentName, p provider.Provider, history []message.Message, ts []tools.BaseTool) ([]message.Message, error) {
	system, reserve := configuredPrompt(name, p.Model())
	probe := &agent{systemMessage: system, maxOutputTokens: reserve}
	status, err := probe.estimateProviderContext(p, history, ts)
	if err != nil {
		return nil, err
	}
	if !status.Fits {
		history, _ = compactHistory(history)
		status, err = probe.estimateProviderContext(p, history, ts)
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
		if footprint.CompatibleTokenizer != "" {
			status.EstimatedInputTokens = footprint.TokenizedPayloadTokens + int64(len(history))*32 + 256
			status.ToolSchemaTokens = footprint.TokenizedSchemaTokens
			status.Method = "compatible " + footprint.CompatibleTokenizer + " tokenizer on serialized JSON + estimated chat overhead; not exact model input"
		}
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
	status.uncalibratedInput = status.EstimatedInputTokens
	status = a.calibratedEstimate(p.Model(), status)
	evidence := int64(0)
	for _, msg := range history {
		if msg.Role == message.Tool {
			raw, _ := json.Marshal(msg)
			evidence += estimateTokens(raw)
		}
	}
	instruction := status.Components["instructions"]
	hist := max(0, status.Components["history_and_evidence"]-evidence)
	schemas := status.ToolSchemaTokens
	base := max(1, instruction+hist+schemas+evidence)
	scale := float64(status.EstimatedInputTokens) / float64(base)
	status.Components = map[string]int64{"instructions": int64(float64(instruction) * scale), "history": int64(float64(hist) * scale), "evidence": int64(float64(evidence) * scale), "schemas": int64(float64(schemas) * scale), "output": status.ReservedOutputTokens, "safety": status.SafetyTokens}
	usable := max(0, status.ContextWindow-status.ReservedOutputTokens-status.SafetyTokens)
	status.ComponentBudgets = map[string]int64{"instructions": usable / 4, "schemas": usable / 4, "history_and_evidence": usable / 2}
	status.Method += "; component shares are planning estimates, total budget is enforced"

	return status, nil
}

func runProvider(a *agent, run *generationRun) provider.Provider {
	if run != nil && run.provider != nil {
		return run.provider
	}
	return a.provider
}

func compactHistoricalMessage(msg message.Message) (message.Message, bool) {
	if msg.Role == message.User || msg.Role == message.System {
		return msg, false
	}
	changed := false
	for i, part := range msg.Parts {
		next, modified := compactHistoricalPart(part)
		if !modified {
			continue
		}
		if !changed {
			msg.Parts = slices.Clone(msg.Parts)
			changed = true
		}
		msg.Parts[i] = next
	}
	return msg, changed
}
func compactHistoricalPart(part message.ContentPart) (message.ContentPart, bool) {
	switch p := part.(type) {
	case message.TextContent:
		if len(p.Text) > 384 {
			p.Text = clip(p.Text, 384) + "\n[Older assistant text abbreviated; retrieve the saved conversation for exact details.]"
			return p, true
		}
	case message.ReasoningContent:
		if p.Thinking != "" {
			return message.ReasoningContent{Thinking: "[Earlier reasoning omitted]"}, true
		}
	case message.ToolResult:
		if len(p.Content) > 256 {
			p.Content = clip(p.Content, 256) + "\n[Older tool result abbreviated. This is historical data, not current file evidence.]"
			p.Metadata = ""
			return p, true
		}
	}
	return part, false
}

func (a *agent) compactRequestHistory(p provider.Provider, history []message.Message, status ContextStatus, ts []tools.BaseTool, compact func([]message.Message) ([]message.Message, int)) ([]message.Message, ContextStatus, error) {
	compacted, count := compact(history)
	if count == 0 {
		return history, status, nil
	}
	next, err := a.estimateProviderContext(p, compacted, ts)
	if err != nil {
		return nil, next, err
	}
	next.CompactedMessages = status.CompactedMessages + count
	return compacted, next, nil
}
