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
	var view, index, inventory tools.BaseTool
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
	if view == nil {
		return history, nil
	}
	// A topical question uses bounded navigation followed by real numbered reads.
	// Index previews themselves never acquire source-read authority.
	if query := tools.ProjectEvidenceQuery(question); query != "" && tools.ProjectInquiry(question) && index != nil && len(tools.ProjectSourcePaths(question)) == 0 {
		if strings.Contains(query, "languages") && tools.OpcodeWorkspace(ctx) {
			query = "localParsers"
			if inventory != nil {
				var err error
				history, _, err = a.bootstrapCall(ctx, session, inventory, `{"summary":true,"limit":6}`, history, run)
				if err != nil {
					return history, err
				}
			}
		}
		input, _ := json.Marshal(map[string]any{"query": query, "limit": 2, "intent": "implementation", "strategy": "lexical"})
		var response tools.ToolResponse
		var err error
		history, response, err = a.bootstrapCall(ctx, session, index, string(input), history, run)
		if err != nil {
			return history, err
		}
		var results struct {
			Results []struct {
				Path   string `json:"path"`
				Symbol string `json:"symbol"`
				Start  int    `json:"start_line"`
				End    int    `json:"end_line"`
				Hash   string `json:"file_hash"`
			} `json:"results"`
		}
		if !response.IsError && json.Unmarshal([]byte(response.Content), &results) == nil {
			seen := map[string]bool{}
			read := false
			for _, hit := range results.Results[:min(2, len(results.Results))] {
				if !filepath.IsLocal(hit.Path) || hit.Start < 1 || seen[hit.Path] {
					continue
				}
				seen[hit.Path] = true
				limit := 60
				if hit.End >= hit.Start {
					limit = min(60, hit.End-hit.Start+3)
				}
				input, _ := json.Marshal(tools.ViewParams{FilePath: hit.Path, Offset: max(0, hit.Start-3), Limit: limit})
				history, response, err = a.bootstrapCall(ctx, session, view, string(input), history, run)
				if err != nil {
					return history, err
				}
				receipt, visible := tools.VisibleSourceReadReceipt("view", response.Content, response.Metadata)
				if !response.IsError && visible {
					if run.primaryEvidence == nil {
						run.primaryEvidence = map[string]bool{}
					}
					run.primaryEvidence[normalizedToolPath(hit.Path)] = true
				}
				read = read || (!response.IsError && visible)
				if !response.IsError && visible && hit.Symbol == query && hit.Hash != "" && receipt.Hash == hit.Hash && receipt.StartLine <= hit.Start && receipt.EndLine >= hit.End {
					break // A complete exact declaration is sufficient; avoid unrelated, truncated context.
				}
			}
			if read {
				return history, nil
			}
		}
	}
	for _, path := range tools.ProjectContextPaths(question) {
		if run.toolBytes >= run.limits.ToolBudget {
			break
		}
		input, _ := json.Marshal(tools.ViewParams{FilePath: path, Limit: 80})
		var err error
		var response tools.ToolResponse
		history, response, err = a.bootstrapCall(ctx, session, view, string(input), history, run)
		if err != nil {
			return history, err
		}
		if _, visible := tools.VisibleSourceReadReceipt("view", response.Content, response.Metadata); !response.IsError && visible && len(run.primaryEvidence) == 0 {
			run.primaryEvidence = map[string]bool{normalizedToolPath(path): true}
		}
	}
	return history, nil
}

func (a *agent) bootstrapCall(ctx context.Context, session string, tool tools.BaseTool, input string, history []message.Message, run *generationRun) ([]message.Message, tools.ToolResponse, error) {
	remaining := min(2400, max(0, run.limits.ToolBudget-run.toolBytes))
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
		cloned := false
		for j, part := range msg.Parts {
			r, ok := part.(message.ToolResult)
			if !ok || r.IsError || latest[r.Content+"\x00"+r.Metadata] <= i {
				continue
			}
			if !cloned {
				result[i].Parts = slices.Clone(result[i].Parts)
				cloned = true
			}
			r.Content = "[Older source body omitted; this unchanged source was revalidated and supplied again later in this conversation.]"
			r.Metadata = ""
			result[i].Parts[j] = r
		}
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
			call, ok := calls[result.ToolCallID]
			if !ok || result.IsError || len(result.Content) > 2400 {
				continue
			}
			r, visible := tools.VisibleSourceReadReceipt(name, result.Content, result.Metadata)
			if visible && normalizedToolPath(r.Path) == normalizedToolPath(toolFilePath(call.Input)) && tools.SourceReadCurrent(ctx, r) {
				return tools.ToolResponse{Content: result.Content, Metadata: result.Metadata}, true
			}
		}
	}
	return tools.ToolResponse{}, false
}
