package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/permission"
)

// ProbeIntegration executes only on an explicit Settings test action. It owns an
// ephemeral permission scope, closes the test client, and never installs tools.
func (a *App) ProbeIntegration(ctx context.Context, candidate config.Integration) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.Get() == nil {
		return nil, fmt.Errorf("configuration unavailable")
	}
	if err := config.ValidateIntegration(candidate); err != nil {
		return nil, err
	}
	if a.Permissions == nil {
		return nil, fmt.Errorf("permission service is unavailable")
	}
	scope := "settings-" + uuid.NewString()
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, scope)
	defer a.Permissions.RevokeSession(scope)

	if candidate.Kind == "mcp" {
		return a.probeMCPIntegration(ctx, candidate)
	}
	if candidate.LSP.Disabled {
		return nil, fmt.Errorf("LSP integration is disabled")
	}
	if !a.Permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: scope, ToolName: "test_lsp_connection", Action: "execute", Path: config.WorkingDirectory(), Description: "Start and initialize the configured language server for a connection test", Params: map[string]any{"name": candidate.Name, "command": candidate.LSP.Command, "args": candidate.LSP.Args}}) {
		return nil, permission.ErrorPermissionDenied
	}
	testCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client, err := lsp.NewClient(testCtx, candidate.LSP.Command, candidate.LSP.Args...)
	if err != nil {
		return nil, fmt.Errorf("cannot start language server: %s", sanitizeProbeText(err.Error()))
	}
	defer client.Close()
	result, err := client.InitializeLSPClient(testCtx, config.WorkingDirectory())
	if err != nil {
		return nil, fmt.Errorf("language server initialization failed: %s", sanitizeProbeText(err.Error()))
	}
	return probeCapabilityLines(result.Capabilities)
}

func sanitizeProbeText(s string) string {
	s = tools.RedactCredentials(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if len(s) > 512 {
		s = s[:512]
	}
	return s
}

func (a *App) probeMCPIntegration(ctx context.Context, candidate config.Integration) ([]string, error) {
	result, err := agent.ProbeMCPServer(ctx, candidate.Name, candidate.MCP, a.Permissions)
	if err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf("Connected · %d tools · %d resources on this page", len(result.Tools), len(result.Resources))}
	for _, name := range result.Tools {
		lines = append(lines, "Tool: "+sanitizeProbeText(name))
	}
	for _, uri := range result.Resources {
		lines = append(lines, "Resource: "+sanitizeProbeText(uri))
	}
	if result.NextToolsCursor != "" || result.NextResourcesCursor != "" {
		lines = append(lines, "More entries exist; the test shows one bounded page.")
	}
	if result.ResourceError != "" {
		lines = append(lines, "Resources: "+sanitizeProbeText(result.ResourceError))
	}
	return lines, nil
}

func probeCapabilityLines(capabilities any) ([]string, error) {
	raw, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("cannot read language server capabilities")
	}
	var caps map[string]json.RawMessage
	if json.Unmarshal(raw, &caps) != nil {
		return nil, fmt.Errorf("invalid language server capabilities")
	}
	names := []string{}
	for name, value := range caps {
		if string(value) != "null" && string(value) != "false" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	lines := []string{fmt.Sprintf("Connected · %d declared capabilities", len(names))}
	for _, name := range names {
		lines = append(lines, "Capability: "+sanitizeProbeText(name))
	}
	return lines, nil
}
