package agent

import (
	"context"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/prompt"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/version"
	"sort"
)

type RuntimeDiagnosticControl interface {
	RuntimeDiagnostic(context.Context, string) (map[string]any, error)
}

func (a *agent) RuntimeDiagnostic(ctx context.Context, session string) (map[string]any, error) {
	status, err := a.ContextStatus(ctx, session)
	if err != nil {
		return nil, err
	}
	a.runMu.Lock()
	model := a.provider.Model()
	a.runMu.Unlock()
	names := []string{}
	for _, t := range a.tools {
		names = append(names, t.Info().Name)
	}
	sort.Strings(names)
	index, indexErr := tools.ControlCodeIndex(ctx, "status", "")
	indexError := ""
	if indexErr != nil {
		indexError = "unavailable; use opcode index status"
	}
	usage, err := a.Usage(ctx, session)
	if err != nil {
		return nil, err
	}
	return map[string]any{"usage": usage, "version": version.Version, "cwd": config.WorkingDirectory(), "model": tools.RedactCredentials(string(model.ID)), "protocol": model.API, "context": status, "prompt_version": prompt.Version, "available_tools": names, "index": index, "index_error": indexError, "credentials": "omitted; endpoint/auth/environment values are never included"}, nil
}
