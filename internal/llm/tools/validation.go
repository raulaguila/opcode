package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/workflow"
)

type workflowStoreKey struct{}

func WithWorkflowStore(ctx context.Context, store *workflow.Store) context.Context {
	return context.WithValue(ctx, workflowStoreKey{}, store)
}

type validationTool struct{ name string }

func NewValidationTools() []BaseTool {
	return []BaseTool{&validationTool{"discover_checks"}, &validationTool{"get_validation_report"}}
}
func (t *validationTool) Info() ToolInfo {
	return ToolInfo{Name: t.name, Description: "Discover candidate checks from bounded root manifests without running commands or editing configuration; proposals require explicit configuration and ordinary run_checks permission. get_validation_report shows existing receipts and freshness for this session, never treating suggestions as passing checks.", Parameters: map[string]any{}}
}
func (t *validationTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	if !json.Valid([]byte(call.Input)) {
		return NewTextErrorResponse("invalid parameters"), nil
	}
	cfg := config.Get()
	configured := map[string]string{}
	if cfg != nil {
		for name, check := range cfg.Checks {
			configured[name] = check.Command
		}
	}
	if t.name == "get_validation_report" {
		session, _ := GetContextValues(ctx)
		store, _ := ctx.Value(workflowStoreKey{}).(*workflow.Store)
		if store == nil {
			return jsonToolResult(map[string]any{"configured": configured, "available": false, "message": "task receipt storage is unavailable"}), nil
		}
		digest, digestErr := WorkspaceDigest(ctx)
		if digestErr != nil {
			return NewTextErrorResponse("validation freshness cannot be determined: " + digestErr.Error()), nil
		}
		task, err := store.Observe(ctx, session, digest)
		if err != nil {
			return ToolResponse{}, err
		}
		return jsonToolResult(map[string]any{"configured": configured, "task_revision": task.Revision, "task_digest": task.Digest, "receipts": task.Validation, "validation_problem": task.ValidationProblem(configured)}), nil
	}
	type candidate struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Source  string `json:"source"`
	}
	proposals := []candidate{}
	for _, manifest := range []string{"go.mod", "package.json", "pyproject.toml", "Cargo.toml", "Makefile"} {
		if ctx.Err() != nil {
			return ToolResponse{}, ctx.Err()
		}
		path := filepath.Join(config.WorkingDirectory(), manifest)
		f, err := openFile(ctx, path)
		if err != nil {
			continue
		}
		f.Close()
		raw, err := readFile(ctx, path)
		if err != nil {
			return ToolResponse{}, err
		}
		if len(raw) > 256<<10 {
			continue
		}
		switch manifest {
		case "go.mod":
			proposals = append(proposals, candidate{"test", "go test ./...", manifest}, candidate{"vet", "go vet ./...", manifest})
		case "Cargo.toml":
			proposals = append(proposals, candidate{"test", "cargo test", manifest}, candidate{"check", "cargo check", manifest})
		case "pyproject.toml":
			proposals = append(proposals, candidate{"test", "python -m pytest", manifest})
		case "package.json":
			var pkg struct{ Scripts map[string]string }
			if json.Unmarshal(raw, &pkg) != nil {
				continue
			}
			names := []string{}
			for name := range pkg.Scripts {
				if name == "test" || name == "lint" || name == "typecheck" || name == "check" {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			for _, name := range names {
				proposals = append(proposals, candidate{name, "npm run " + name, manifest})
			}
		case "Makefile":
			for _, line := range strings.Split(string(raw), "\n") {
				for _, name := range []string{"test", "check", "lint"} {
					if strings.HasPrefix(line, name+":") {
						proposals = append(proposals, candidate{name, "make " + name, manifest})
					}
				}
			}
		}
	}
	return jsonToolResult(map[string]any{"configured": configured, "proposals": proposals, "executed": false, "configured_automatically": false}), nil
}
