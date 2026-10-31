package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

func (a *agent) bootstrap(ctx context.Context, session, question string, history []message.Message, run *generationRun) ([]message.Message, error) {
	if a.workflow == nil {
		return history, nil
	}
	view, index, inventory := bootstrapTools(run)
	if view == nil {
		return history, nil
	}
	query := tools.ProjectEvidenceQuery(question)
	if query != "" && tools.ProjectInquiry(question) && index != nil && len(tools.ProjectSourcePaths(question)) == 0 {
		b := evidenceBootstrap{agent: a, ctx: ctx, session: session, run: run, history: history, view: view}
		query, err := b.topicQuery(query, inventory)
		if err != nil {
			return b.history, err
		}
		read, err := b.readIndex(query, index)
		history = b.history
		if err != nil || read {
			return history, err
		}
	}
	history, err := a.bootstrapBasic(ctx, session, question, history, run, view)
	if err != nil || index == nil || !tools.ProjectOverview(question) {
		return history, err
	}
	b := evidenceBootstrap{agent: a, ctx: ctx, session: session, run: run, history: history, view: view, overview: true}
	_, err = b.readIndex(overviewQuery(history), index)
	return b.history, err
}

func (a *agent) bootstrapCall(ctx context.Context, session string, tool tools.BaseTool, input string, history []message.Message, run *generationRun) ([]message.Message, tools.ToolResponse, error) {
	remaining := min(2400, max(0, run.limits.ToolBudget-run.toolBytes))
	if tool.Info().Name == "search_code_index" {
		remaining = min(6000, max(0, run.limits.ToolBudget-run.toolBytes))
	}
	if remaining == 0 {
		return history, tools.NewTextErrorResponse("Context preparation budget exhausted; inspect sources progressively."), nil
	}
	name := tool.Info().Name
	response, reused := reusableSource(ctx, history, name, input)
	// An explicit request to call a tool requires a new actual call, not a cache hit.
	if run.requiredCalls[name] > 0 {
		reused = false
	}
	prefix := "context-"
	if reused {
		prefix = "context-reused-"
	}
	origin := "context"
	if reused {
		origin = "context-reused"
	}
	call := message.ToolCall{ID: fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()), Origin: origin, Name: name, Input: input, Finished: true}
	assistant, err := a.messages.Create(ctx, session, message.CreateMessageParams{Role: message.Assistant, Model: a.Model().ID, Parts: []message.ContentPart{call}})
	if err != nil {
		return history, response, err
	}
	if !reused {
		toolCtx := context.WithValue(ctx, tools.SessionIDContextKey, session)
		toolCtx = context.WithValue(toolCtx, tools.MessageIDContextKey, assistant.ID)
		response, err = tool.Run(toolCtx, tools.ToolCall{ID: call.ID, Name: name, Input: input})
		if err != nil {
			response = tools.NewTextErrorResponse(err.Error())
		}
	}
	response.Content = clip(response.Content, remaining)
	run.toolBytes += len(response.Content)
	if len(response.Metadata) > min(4096, max(0, run.limits.ToolBudget-run.toolBytes)) {
		response.Metadata = ""
	}
	run.toolBytes += len(response.Metadata)
	a.observePreparation(session, name, reused, len(response.Content)+len(response.Metadata))
	a.finishMessage(ctx, &assistant, message.FinishReasonToolUse)
	result, err := a.messages.Create(ctx, session, message.CreateMessageParams{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: call.ID, Name: name, Content: response.Content, Metadata: response.Metadata, IsError: response.IsError}}})
	if err != nil {
		return history, response, err
	}
	return append(history, assistant, result), response, nil
}

// Re-expose a revalidated source only once in the provider projection. Keep all
// tool pairs, inputs, signed state and the persisted transcript intact.
func projectReusedSources(history []message.Message) []message.Message {
	reused := map[string]bool{}
	for _, msg := range history {
		for _, call := range msg.ToolCalls() {
			if call.Origin == "context-reused" {
				reused[call.ID] = true
			}
		}
	}
	latest := map[string]int{}
	for i, msg := range history {
		for _, r := range msg.ToolResults() {
			if reused[r.ToolCallID] && !r.IsError && r.Metadata != "" {
				latest[r.Content+"\x00"+r.Metadata] = i
			}
		}
	}
	result := slices.Clone(history)
	for i, msg := range history {
		result[i] = abbreviateReusedMessage(msg, i, latest)
	}
	return result
}

func reusableSource(ctx context.Context, history []message.Message, name, input string) (tools.ToolResponse, bool) {
	if name != "view" {
		return tools.ToolResponse{}, false
	}
	calls := map[string]message.ToolCall{}
	for _, msg := range history {
		for _, call := range msg.ToolCalls() {
			if call.Name == name && call.Input == input {
				calls[call.ID] = call
			}
		}
	}
	for i := len(history) - 1; i >= 0; i-- {
		for _, result := range history[i].ToolResults() {
			if response, ok := reusableSourceResult(ctx, name, calls, result); ok {
				return response, true
			}
		}
	}
	return tools.ToolResponse{}, false
}

func bootstrapTools(run *generationRun) (view, index, inventory tools.BaseTool) {
	for _, tool := range run.available {
		if !runAllowsTool(run, tool) || tools.IsOpaqueTool(tool) {
			continue
		}
		switch tool.Info().Name {
		case tools.ViewToolName:
			view = tool
		case "search_code_index":
			index = tool
		case "get_language_support":
			inventory = tool
		}
	}
	return
}

type projectEvidenceHit struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
	Start  int    `json:"start_line"`
	End    int    `json:"end_line"`
	Hash   string `json:"file_hash"`
}
type evidenceBootstrap struct {
	agent    *agent
	ctx      context.Context
	session  string
	run      *generationRun
	history  []message.Message
	view     tools.BaseTool
	read     bool
	overview bool
}

func (b *evidenceBootstrap) topicQuery(query string, inventory tools.BaseTool) (string, error) {
	if !strings.Contains(query, "languages") || !tools.OpcodeWorkspace(b.ctx) {
		return query, nil
	}
	query = "localParsers"
	if inventory == nil {
		return query, nil
	}
	history, _, err := b.agent.bootstrapCall(b.ctx, b.session, inventory, `{"summary":true,"limit":6}`, b.history, b.run)
	b.history = history
	return query, err
}
func (b *evidenceBootstrap) readIndex(query string, index tools.BaseTool) (bool, error) {
	limit := 2
	intent := "implementation"
	if b.overview {
		limit = 4
		intent = "overview"
	}
	input, _ := json.Marshal(map[string]any{"query": query, "limit": limit, "intent": intent, "strategy": "lexical"})
	history, response, err := b.agent.bootstrapCall(b.ctx, b.session, index, string(input), b.history, b.run)
	b.history = history
	if err != nil {
		return false, err
	}
	var result struct {
		Results []projectEvidenceHit `json:"results"`
	}
	if response.IsError || json.Unmarshal([]byte(response.Content), &result) != nil {
		return false, nil
	}
	return b.readIndexHits(query, result.Results[:min(limit, len(result.Results))])
}

func (b *evidenceBootstrap) readIndexHits(query string, hits []projectEvidenceHit) (bool, error) {
	seen := map[string]bool{}
	if b.overview {
		seen = overviewReadPaths(b.run, b.history)
	}
	attempts := 0
	for _, hit := range hits {
		key := normalizedToolPath(hit.Path)
		if !filepath.IsLocal(hit.Path) || hit.Start < 1 || seen[key] {
			continue
		}
		seen[key] = true
		attempts++
		complete, err := b.readHit(query, hit)
		if err != nil {
			return false, err
		}
		if complete && !b.overview || attempts == 2 {
			break
		}
	}
	return b.read, nil
}
func (b *evidenceBootstrap) readHit(query string, hit projectEvidenceHit) (bool, error) {
	limit := 60
	if hit.End >= hit.Start {
		limit = min(60, hit.End-hit.Start+3)
	}
	input, _ := json.Marshal(tools.ViewParams{FilePath: hit.Path, Offset: max(0, hit.Start-3), Limit: limit})
	history, response, err := b.agent.bootstrapCall(b.ctx, b.session, b.view, string(input), b.history, b.run)
	b.history = history
	if err != nil {
		return false, err
	}
	receipt, visible := tools.VisibleSourceReadReceipt("view", response.Content, response.Metadata)
	if response.IsError || !visible {
		return false, nil
	}
	if b.run.primaryEvidence == nil {
		b.run.primaryEvidence = map[string]bool{}
	}
	b.run.primaryEvidence[normalizedToolPath(hit.Path)] = true
	b.read = true
	return hit.Symbol == query && hit.Hash != "" && receipt.Hash == hit.Hash && receipt.StartLine <= hit.Start && receipt.EndLine >= hit.End, nil
}
func (a *agent) bootstrapBasic(ctx context.Context, session, question string, history []message.Message, run *generationRun, view tools.BaseTool) ([]message.Message, error) {
	for _, path := range tools.ProjectContextPaths(question) {
		if run.toolBytes >= run.limits.ToolBudget {
			break
		}
		limit := 80
		if tools.ProjectOverview(question) {
			limit = overviewSourceLimit(path, question)
		}
		input, _ := json.Marshal(tools.ViewParams{FilePath: path, Limit: limit})
		next, response, err := a.bootstrapCall(ctx, session, view, string(input), history, run)
		history = next
		if err != nil {
			return history, err
		}
		if _, visible := tools.VisibleSourceReadReceipt("view", response.Content, response.Metadata); !response.IsError && visible && len(run.primaryEvidence) == 0 {
			run.primaryEvidence = map[string]bool{normalizedToolPath(path): true}
		}
	}
	return history, nil
}

func abbreviateReusedMessage(msg message.Message, index int, latest map[string]int) message.Message {
	result := msg
	i := index

	cloned := false
	for j, part := range msg.Parts {
		r, ok := part.(message.ToolResult)
		if !ok || r.IsError || latest[r.Content+"\x00"+r.Metadata] <= i {
			continue
		}
		if !cloned {
			result.Parts = slices.Clone(result.Parts)
			cloned = true
		}
		r.Content = "[Older source body omitted; this unchanged source was revalidated and supplied again later in this conversation.]"
		r.Metadata = ""
		result.Parts[j] = r
	}

	return result
}

func reusableSourceResult(ctx context.Context, name string, calls map[string]message.ToolCall, result message.ToolResult) (tools.ToolResponse, bool) {
	call, ok := calls[result.ToolCallID]
	if !ok || result.IsError || len(result.Content) > 2400 {
		return tools.ToolResponse{}, false
	}
	r, visible := tools.VisibleSourceReadReceipt(name, result.Content, result.Metadata)
	if visible && normalizedToolPath(r.Path) == normalizedToolPath(toolFilePath(call.Input)) && tools.SourceReadCurrent(ctx, r) {
		return tools.ToolResponse{Content: result.Content, Metadata: result.Metadata}, true
	}
	return tools.ToolResponse{}, false
}
