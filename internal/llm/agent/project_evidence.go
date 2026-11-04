package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

// bootstrap reads only the files the question names. Guessing sources from its
// words read unrelated code; the model searches for what it needs.
func (a *agent) bootstrap(ctx context.Context, session, question string, history []message.Message, run *generationRun) ([]message.Message, error) {
	if a.workflow == nil {
		return history, nil
	}
	view := bootstrapView(run)
	if view == nil {
		return history, nil
	}
	return a.bootstrapBasic(ctx, session, question, history, run, view)
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
	response.Content = tools.Clip(response.Content, remaining)
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

func bootstrapView(run *generationRun) tools.BaseTool {
	for _, tool := range run.available {
		if tool.Info().Name == tools.ViewToolName && runAllowsTool(run, tool) && !tools.IsOpaqueTool(tool) {
			return tool
		}
	}
	return nil
}

func (a *agent) bootstrapBasic(ctx context.Context, session, question string, history []message.Message, run *generationRun, view tools.BaseTool) ([]message.Message, error) {
	for _, path := range tools.ProjectContextPaths(question) {
		if run.toolBytes >= run.limits.ToolBudget {
			break
		}
		input, _ := json.Marshal(tools.ViewParams{FilePath: path, Limit: 80})
		next, _, err := a.bootstrapCall(ctx, session, view, string(input), history, run)
		history = next
		if err != nil {
			return history, err
		}
	}
	return a.bootstrapImporters(ctx, session, question, history, run)
}

// bootstrapImporters shows who imports the package of the first Go file the
// question mentions, so the answer can place the file in the project.
// shortcut: Go only and one package per question; other languages leave it to
// the model's own search.
func (a *agent) bootstrapImporters(ctx context.Context, session, question string, history []message.Message, run *generationRun) ([]message.Message, error) {
	var grep tools.BaseTool
	for _, tool := range run.available {
		if tool.Info().Name == tools.GrepToolName && runAllowsTool(run, tool) {
			grep = tool
		}
	}
	if grep == nil || run.toolBytes >= run.limits.ToolBudget {
		return history, nil
	}
	for _, path := range tools.MentionedPaths(question) {
		if pkg := goImportPath(config.WorkingDirectory(), path); pkg != "" {
			input, _ := json.Marshal(tools.GrepParams{Pattern: `"` + pkg + `"`, Include: "*.go", LiteralText: true})
			history, _, err := a.bootstrapCall(ctx, session, grep, string(input), history, run)
			return history, err
		}
	}
	return history, nil
}

// goImportPath is the import path of the package holding a Go file, or "" for
// non-Go files, commands (package main) and workspaces without go.mod.
func goImportPath(root, path string) string {
	if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
		return ""
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	module := ""
	for line := range strings.SplitSeq(string(mod), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			module = strings.Trim(strings.TrimSpace(name), `"`)
			break
		}
	}
	source, err := os.ReadFile(filepath.Join(root, path))
	if module == "" || err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(source), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "package "); ok {
			if strings.TrimSpace(name) == "main" {
				return ""
			}
			break
		}
	}
	if dir := filepath.ToSlash(filepath.Dir(path)); dir != "." {
		return module + "/" + dir
	}
	return module
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
