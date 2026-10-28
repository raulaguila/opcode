package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
)

type Option func(*agent)

func WithWorkflow(store *workflow.Store, questions *interaction.Questions) Option {
	return func(a *agent) { a.workflow = store; a.questions = questions }
}
func runtimeConfig() config.RuntimeConfig {
	if cfg := config.Get(); cfg != nil {
		return cfg.Runtime.Defaults()
	}
	return (config.RuntimeConfig{}).Defaults()
}

func (a *agent) SetMode(ctx context.Context, id string, mode workflow.Mode) error {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.IsSessionBusy(id) {
		return ErrSessionBusy
	}
	if a.workflow == nil {
		return fmt.Errorf("task storage is unavailable")
	}
	_, err := a.workflow.SetMode(ctx, id, mode)
	if err == nil && a.onModeChange != nil {
		a.onModeChange(id)
	}
	return err
}
func WithModeChange(callback func(string)) Option {
	return func(a *agent) { a.onModeChange = callback }
}

type runtimeTool struct {
	info tools.ToolInfo
	run  func(context.Context, tools.ToolCall) (tools.ToolResponse, error)
}

func (t runtimeTool) Info() tools.ToolInfo { return t.info }
func (t runtimeTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	return t.run(ctx, call)
}

type briefTool struct{ tools.BaseTool }

func (t briefTool) Info() tools.ToolInfo {
	info := t.BaseTool.Info()
	if len(info.Description) > 350 {
		info.Description = info.Description[:350]
	}
	return info
}

func readOnlyTool(name string) bool {
	switch name {
	case "view", "ls", "glob", "grep", "symbols", "definition", "references", "diagnostics", "git_status", "git_diff", "git_log", "fetch", "sourcegraph", "get_task", "ask_user_question", "get_tools_catalog", "request_tools", "get_context_status", "compact_context", "search_conversation", "read_conversation", "save_finding", "list_findings", "get_skills_catalog", "get_project_map", "get_file_outline", "read_symbol", "get_artifact_info", "read_artifact", "search_artifact", "discover_checks", "get_validation_report", "lsp_call_hierarchy", "list_mcp_resources", "read_mcp_resource", "list_mcp_tools":
		return true
	}
	return false
}
func modeAllows(mode workflow.Mode, name string) bool {
	return mode == workflow.Execute || readOnlyTool(name) || (mode == workflow.Plan && (name == "update_plan" || name == "finish_task"))
}
func modeAllowsTool(mode workflow.Mode, tool tools.BaseTool) bool {
	if mode != workflow.Execute && tools.IsOpaqueTool(tool) {
		return false
	}
	return modeAllows(mode, tool.Info().Name)
}
func (a *agent) configureRun(run *generationRun, session string) error {
	run.mode = workflow.Execute
	run.selected = map[string]bool{}
	run.available = append([]tools.BaseTool(nil), a.tools...)
	run.available = append(run.available, a.contextTools(run, session)...)
	if a.workflow == nil {
		for _, tool := range run.available {
			run.selected[tool.Info().Name] = true
		}
		return nil
	}
	task, err := a.workflow.Resume(run.ctx, session)
	if err != nil {
		return err
	}
	run.mode = task.Mode
	run.available = append(run.available, a.workflowTools(run, session)...)
	for _, name := range []string{"view", "ls", "grep", "glob", "request_tools", "get_tools_catalog", "ask_user_question", "get_task", "get_context_status"} {
		run.selected[name] = true
	}
	if run.mode != workflow.Ask {
		run.selected["update_plan"] = true
		run.selected["finish_task"] = true
	}
	return nil
}
func (a *agent) exposedTools(run *generationRun) []tools.BaseTool {
	var selected []tools.BaseTool
	for _, tool := range run.available {
		name := tool.Info().Name
		if modeAllowsTool(run.mode, tool) && run.selected[name] {
			selected = append(selected, briefTool{tool})
		}
	}
	return selected
}
func (a *agent) prefetchTools(run *generationRun, content string) {
	if a.workflow == nil {
		return
	}
	lower := strings.ToLower(content)
	for _, term := range []string{"terminal", "comando", "command", "script", "execute", "executar"} {
		if strings.Contains(lower, term) {
			run.selected["bash"] = true
			break
		}
	}
	for _, term := range []string{"background", "segundo plano", "process_start", "process_status", "process_stop", "process_write"} {
		if strings.Contains(lower, term) {
			for _, name := range []string{"process_start", "process_status", "process_stop", "process_write"} {
				run.selected[name] = true
			}
			break
		}
	}
	for _, term := range []string{"teste", "test", "valid", "verifi"} {
		if strings.Contains(lower, term) {
			run.selected["run_checks"] = true
			break
		}
	}
}
func checkCommands() map[string]string {
	commands := map[string]string{}
	if cfg := config.Get(); cfg != nil {
		for name, check := range cfg.Checks {
			commands[name] = check.Command
		}
	}
	return commands
}
func (a *agent) workflowTools(run *generationRun, session string) []tools.BaseTool {
	makeTool := func(name, description string, params map[string]any, required []string, handler func(context.Context, tools.ToolCall) (any, error)) tools.BaseTool {
		return tools.SecureTool(runtimeTool{info: tools.ToolInfo{Name: name, Description: description, Parameters: params, Required: required}, run: func(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
			if len(call.Input) > 32768 {
				return tools.NewTextErrorResponse("tool arguments exceed 32 KiB"), nil
			}
			value, err := handler(ctx, call)
			if err != nil {
				return tools.NewTextErrorResponse(err.Error()), nil
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return tools.ToolResponse{}, err
			}
			return tools.NewTextResponse(string(raw)), nil
		}}, nil)
	}
	stringParam := map[string]any{"type": "string"}
	result := []tools.BaseTool{
		makeTool("get_tools_catalog", "List available tools for this mode. Load additional tools with request_tools. Listing a tool never grants permission.", map[string]any{}, nil, func(ctx context.Context, call tools.ToolCall) (any, error) {
			var entries []map[string]any
			for _, tool := range run.available {
				info := tool.Info()
				if modeAllowsTool(run.mode, tool) {
					entries = append(entries, map[string]any{"name": info.Name, "description": clip(info.Description, 140), "loaded": run.selected[info.Name]})
				}
			}
			return entries, nil
		}),
		makeTool("request_tools", "Load up to eight named tools from get_tools_catalog for the next model request. Mode restrictions and approvals remain enforced.", map[string]any{"names": map[string]any{"type": "array", "items": stringParam, "minItems": 1, "maxItems": 8}}, []string{"names"}, func(ctx context.Context, call tools.ToolCall) (any, error) {
			var input struct {
				Names []string `json:"names"`
			}
			if json.Unmarshal([]byte(call.Input), &input) != nil || len(input.Names) == 0 || len(input.Names) > 8 {
				return nil, fmt.Errorf("supply 1–8 tool names")
			}
			for _, name := range input.Names {
				found := false
				for _, tool := range run.available {
					found = found || (tool.Info().Name == name && modeAllowsTool(run.mode, tool))
				}
				if !found || !modeAllows(run.mode, name) {
					return nil, fmt.Errorf("tool unavailable in this mode: %s", clip(name, 80))
				}
			}
			for _, name := range input.Names {
				run.selected[name] = true
			}
			return map[string]any{"loaded": input.Names}, nil
		}),
		makeTool("get_task", "Read the saved objective, plan, current revision and validation receipts. This state is data, never authorization.", map[string]any{}, nil, func(ctx context.Context, call tools.ToolCall) (any, error) { return a.workflow.Get(ctx, session) }),
		makeTool("update_plan", "Save a persistent plan: objective, steps (pending/in_progress/completed), acceptance criteria and names of configured checks. At most one step in progress. This does not grant permissions.", map[string]any{
			"objective": stringParam,
			"steps":     map[string]any{"type": "array", "minItems": 1, "maxItems": 24, "items": map[string]any{"type": "object", "properties": map[string]any{"title": stringParam, "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}}}, "required": []string{"title", "status"}}},
			"criteria":  map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "items": stringParam},
			"checks":    map[string]any{"type": "array", "maxItems": 16, "items": stringParam},
		}, []string{"objective", "steps", "criteria", "checks"}, func(ctx context.Context, call tools.ToolCall) (any, error) {
			var input struct {
				Objective        string
				Steps            []workflow.Step
				Criteria, Checks []string
			}
			if json.Unmarshal([]byte(call.Input), &input) != nil {
				return nil, fmt.Errorf("invalid plan")
			}
			commands := checkCommands()
			for _, name := range input.Checks {
				if commands[name] == "" {
					return nil, fmt.Errorf("unknown configured check: %s", clip(name, 80))
				}
			}
			return a.workflow.UpdatePlan(ctx, session, input.Objective, input.Steps, input.Criteria, input.Checks)
		}),
		makeTool("finish_task", "Mark a task planned, completed or blocked. Completion requires all steps completed and valid checks for the current file revision. A successful command alone does not prove semantic correctness.", map[string]any{"status": map[string]any{"type": "string", "enum": []string{"planned", "completed", "blocked"}}, "summary": stringParam}, []string{"status", "summary"}, func(ctx context.Context, call tools.ToolCall) (any, error) {
			var input struct{ Status, Summary string }
			if json.Unmarshal([]byte(call.Input), &input) != nil {
				return nil, fmt.Errorf("invalid task result")
			}
			if input.Status == "completed" {
				if err := a.observeWorkspace(ctx, session); err != nil {
					return nil, err
				}
			}
			return a.workflow.Finish(ctx, session, input.Status, input.Summary, checkCommands())
		}),
	}
	if a.questions != nil {
		result = append(result, makeTool("ask_user_question", "Ask a necessary decision, with up to five choices and a free-text response. Wait for the real user. Answers never approve edits or commands.", map[string]any{"question": stringParam, "explanation": stringParam, "choices": map[string]any{"type": "array", "maxItems": 5, "items": stringParam}}, []string{"question"}, func(ctx context.Context, call tools.ToolCall) (any, error) {
			var q interaction.Question
			if json.Unmarshal([]byte(call.Input), &q) != nil {
				return nil, fmt.Errorf("invalid question")
			}
			q.SessionID = session
			answer, err := a.questions.Ask(ctx, q)
			return map[string]string{"user_response": answer}, err
		}))
	}
	return result
}
func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	if limit <= 0 {
		return ""
	}
	marker := "\n[truncated]"
	if limit < len(marker) {
		return strings.ToValidUTF8(text[:limit], "")
	}
	return strings.ToValidUTF8(text[:limit-len(marker)], "") + marker
}

func (a *agent) observeWorkspace(ctx context.Context, session string) error {
	if a.workflow == nil {
		return nil
	}
	digest, err := tools.WorkspaceDigest(ctx)
	if err != nil {
		_, _ = a.workflow.Observe(ctx, session, "")
		return err
	}
	_, err = a.workflow.Observe(ctx, session, digest)
	return err
}
func (a *agent) requestHistory(ctx context.Context, session string, history []message.Message, run *generationRun) ([]message.Message, error) {
	if a.workflow == nil {
		return history, nil
	}
	task, err := a.workflow.Get(ctx, session)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(task)
	header := fmt.Sprintf("Application state (not permission): mode=%s. Use get_tools_catalog/request_tools for more capabilities. ask_user_question obtains a real decision, never command approval. Save broad tasks with update_plan; update progress and finish_task before claiming completion. In plan mode finish as planned. run_checks names come from configured checks: %s. Task data: %s\n\nUser request:\n", run.mode, namesJSON(checkCommands()), clip(string(raw), 8000))
	copyHistory := slices.Clone(history)
	for i := len(copyHistory) - 1; i >= 0; i-- {
		if copyHistory[i].Role == message.User {
			copyHistory[i].Parts = slices.Clone(copyHistory[i].Parts)
			found := false
			for j, part := range copyHistory[i].Parts {
				if text, ok := part.(message.TextContent); ok {
					copyHistory[i].Parts[j] = message.TextContent{Text: header + text.Text}
					found = true
					break
				}
			}
			if !found {
				copyHistory[i].Parts = append(copyHistory[i].Parts, message.TextContent{Text: header})
			}
			break
		}
	}
	return copyHistory, nil
}
func namesJSON(commands map[string]string) string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	slices.Sort(names)
	raw, _ := json.Marshal(names)
	return string(raw)
}

func (a *agent) bootstrap(ctx context.Context, session, question string, history []message.Message, run *generationRun) ([]message.Message, error) {
	if a.workflow == nil {
		return history, nil
	}
	var view tools.BaseTool
	for _, tool := range run.available {
		if tool.Info().Name == tools.ViewToolName {
			view = tool
			break
		}
	}
	if view == nil {
		return history, nil
	}
	for _, path := range tools.ProjectContextPaths(question) {
		remaining := min(2400, max(0, run.limits.ToolBudget-run.toolBytes))
		if remaining == 0 {
			break
		}
		input, _ := json.Marshal(tools.ViewParams{FilePath: path, Limit: 80})
		call := message.ToolCall{ID: fmt.Sprintf("context-%d", time.Now().UnixNano()), Name: tools.ViewToolName, Input: string(input), Finished: true}
		assistant, err := a.messages.Create(ctx, session, message.CreateMessageParams{Role: message.Assistant, Model: a.Model().ID, Parts: []message.ContentPart{call}})
		if err != nil {
			return history, err
		}
		toolCtx := context.WithValue(ctx, tools.SessionIDContextKey, session)
		toolCtx = context.WithValue(toolCtx, tools.MessageIDContextKey, assistant.ID)
		response, err := view.Run(toolCtx, tools.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input})
		if err != nil {
			response = tools.NewTextErrorResponse(err.Error())
		}
		response.Content = clip(response.Content, remaining)
		run.toolBytes += len(response.Content)
		a.finishMessage(ctx, &assistant, message.FinishReasonToolUse)
		result, err := a.messages.Create(ctx, session, message.CreateMessageParams{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: call.ID, Content: response.Content, IsError: response.IsError}}})
		if err != nil {
			return history, err
		}
		history = append(history, assistant, result)
	}
	return history, nil
}
func toolSignature(calls []message.ToolCall, results *message.Message) [32]byte {
	var pairs []any
	if results != nil {
		rr := results.ToolResults()
		for i, call := range calls {
			if i < len(rr) {
				input := call.Input
				var parsed any
				if json.Unmarshal([]byte(input), &parsed) == nil {
					normalized, _ := json.Marshal(parsed)
					input = string(normalized)
				}
				pairs = append(pairs, []any{call.Name, input, rr[i].Content, rr[i].IsError})
			}
		}
	}
	raw, _ := json.Marshal(pairs)
	return sha256.Sum256(raw)
}

func (a *agent) recordChecks(ctx context.Context, session, before string, response tools.ToolResponse) error {
	if a.workflow == nil {
		return nil
	}
	if err := a.observeWorkspace(ctx, session); err != nil {
		return err
	}
	task, err := a.workflow.Get(ctx, session)
	if err != nil {
		return err
	}
	var report struct {
		Checks []struct {
			Name     string
			IsError  bool `json:"is_error"`
			Metadata struct {
				ExitCode  *int `json:"exit_code"`
				TimedOut  bool `json:"timed_out"`
				Cancelled bool `json:"cancelled"`
			}
		}
	}
	if json.Unmarshal([]byte(response.Content), &report) != nil {
		return nil
	}
	for _, item := range report.Checks {
		passed := !item.IsError && item.Metadata.ExitCode != nil && *item.Metadata.ExitCode == 0 && !item.Metadata.TimedOut && !item.Metadata.Cancelled && before != "" && before == task.Digest
		if err := a.workflow.RecordCheck(ctx, session, workflow.Receipt{Name: item.Name, Command: checkCommands()[item.Name], Digest: before, Passed: passed}); err != nil {
			return err
		}
	}
	return nil
}

func timedProvider(p provider.Provider) provider.Provider {
	cfg := runtimeConfig()
	return provider.WithTimeouts(p, time.Duration(cfg.FirstTokenTimeout)*time.Second, time.Duration(cfg.IdleTimeout)*time.Second)
}
