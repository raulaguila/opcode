package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
)

type checksTool struct {
	permissions permission.Service
	manager     *ProcessManager
	ownsManager bool
}

func NewChecksTool(permissions permission.Service, managers ...*ProcessManager) BaseTool {
	var manager *ProcessManager
	owned := len(managers) == 0 || managers[0] == nil
	if owned {
		manager = NewProcessManager()
	} else {
		manager = managers[0]
	}
	return &checksTool{permissions: permissions, manager: manager, ownsManager: owned}
}
func (c *checksTool) Info() ToolInfo {
	return ToolInfo{Name: "run_checks", Description: "Run named checks from the user's checks configuration. Every command requires the usual execution authorization and sandbox. Results include exit status, cancellation and bounded output.", Parameters: map[string]any{"names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Names to run; omit to run all configured checks (maximum 16)."}}}
}
func (c *checksTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var input struct{ Names []string }
	if json.Unmarshal([]byte(call.Input), &input) != nil {
		return NewTextErrorResponse("invalid checks parameters"), nil
	}
	cfg := config.Get()
	if cfg == nil || len(cfg.Checks) == 0 {
		return NewTextErrorResponse("no checks configured; add named command entries under checks in opcode.json"), nil
	}
	names, err := configuredCheckNames(cfg.Checks, input.Names)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	type result struct {
		Name     string          `json:"name"`
		Output   string          `json:"output"`
		Metadata json.RawMessage `json:"metadata"`
		IsError  bool            `json:"is_error"`
	}
	results := []result{}
	failed := false
	for _, name := range names {
		if ctx.Err() != nil {
			return ToolResponse{}, ctx.Err()
		}
		entry := cfg.Checks[name]
		input, _ := json.Marshal(BashParams{Command: entry.Command, Timeout: entry.Timeout})
		response, err := SecureTool(NewBashTool(c.permissions, c.manager), c.permissions).Run(ctx, ToolCall{ID: call.ID, Name: BashToolName, Input: string(input)})
		if err != nil {
			return ToolResponse{}, err
		}
		metadata := json.RawMessage(response.Metadata)
		if len(metadata) == 0 {
			metadata = json.RawMessage("null")
		}
		results = append(results, result{Name: name, Output: response.Content, Metadata: metadata, IsError: response.IsError})
		failed = failed || response.IsError
	}
	response := jsonToolResult(map[string]any{"checks": results})
	response.IsError = failed
	return response, nil
}

func (c *checksTool) Close() error {
	if c.ownsManager {
		return c.manager.Close()
	}
	return nil
}
func (c *checksTool) CancelSession(session string) { c.manager.CancelSession(session) }

func configuredCheckNames(checks map[string]config.CheckConfig, requested []string) ([]string, error) {
	names := requested
	if len(names) == 0 {
		for name := range checks {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	if len(names) > 16 {
		return nil, fmt.Errorf("at most 16 checks may run at once")
	}
	for _, name := range names {
		if entry, exists := checks[name]; !exists || entry.Command == "" {
			return nil, fmt.Errorf("unknown or empty configured check")
		}
	}

	return names, nil
}
