package tools

import (
	"context"
	"encoding/json"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
)

type processTool struct {
	name        string
	manager     *ProcessManager
	permissions permission.Service
}

func NewProcessTools(manager *ProcessManager, permissions permission.Service) []BaseTool {
	return []BaseTool{&processTool{"process_start", manager, permissions}, &processTool{"process_status", manager, permissions}, &processTool{"process_stop", manager, permissions}, &processTool{"process_write", manager, permissions}}
}

func (p *processTool) Info() ToolInfo {
	params := map[string]any{"id": map[string]any{"type": "string"}}
	required := []string{"id"}
	if p.name == "process_start" {
		params = map[string]any{"command": map[string]any{"type": "string"}, "timeout": map[string]any{"type": "integer", "maximum": MaxTimeout},
			"pty":  map[string]any{"type": "boolean", "description": "Allocate an interactive terminal; process_write sends input after a separate approval."},
			"cols": map[string]any{"type": "integer"}, "rows": map[string]any{"type": "integer"}}
		required = []string{"command"}
	} else if p.name == "process_write" {
		params["data"] = map[string]any{"type": "string", "description": "Terminal input, including newline to submit a line. Maximum 16384 bytes."}
		required = append(required, "data")
	}
	return ToolInfo{Name: p.name, Description: "Manage a session-owned process with live bounded output and optional PTY. Starts and agent-generated input require separate permissions. Poll process_status for output. Processes expire (default 60s, maximum 10min) and stop on session cancellation/shutdown.", Parameters: params, Required: required}
}

func (p *processTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var input struct {
		ProcessParams
		ID, Data string
	}
	if json.Unmarshal([]byte(call.Input), &input) != nil {
		return NewTextErrorResponse("invalid process parameters"), nil
	}
	owner, _ := GetContextValues(ctx)
	if owner == "" {
		return NewTextErrorResponse("session context required"), nil
	}
	if p.name == "process_start" {
		info, err := p.manager.Start(ctx, p.permissions, input.ProcessParams)
		if err != nil {
			return ToolResponse{}, err
		}
		record, _ := p.manager.lookup(owner, info.ID)
		return p.manager.snapshot(record), nil
	}
	record, err := p.manager.lookup(owner, input.ID)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if p.name == "process_write" {
		return p.writeInput(ctx, owner, record, input.ID, input.Data)
	} else if p.name == "process_stop" {
		if err := p.manager.Stop(ctx, owner, input.ID); err != nil {
			return ToolResponse{}, err
		}
	}
	return p.manager.snapshot(record), nil
}

// A process tool borrows its manager; its application or factory owns shutdown.
func (p *processTool) Close() error                 { return nil }
func (p *processTool) CancelSession(session string) { p.manager.CancelSession(session) }

func (p *processTool) writeInput(ctx context.Context, owner string, record *managedProcess, id, data string) (ToolResponse, error) {

	if len(data) == 0 || len(data) > 16384 || redact(data) != data {
		return NewTextErrorResponse("invalid terminal input or configured credential in input"), nil
	}
	if p.permissions == nil || !p.permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID: owner, ToolName: p.name, Action: "write_stdin:" + record.id, Path: config.WorkingDirectory(),
		Description: "Send input to interactive process", Params: map[string]string{"id": id, "data": data},
	}) {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}
	if err := ctx.Err(); err != nil {
		return ToolResponse{}, err
	}
	if err := p.manager.Input(owner, id, data); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	return p.manager.snapshot(record), nil
}
