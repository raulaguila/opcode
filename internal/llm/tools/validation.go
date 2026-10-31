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
	configured := map[string]string{}
	if cfg := config.Get(); cfg != nil {
		for name, check := range cfg.Checks {
			configured[name] = check.Command
		}
	}
	if t.name == "get_validation_report" {
		return validationReceiptReport(ctx, configured)
	}
	proposals, err := discoverCheckCandidates(ctx)
	if err != nil {
		return ToolResponse{}, err
	}
	return jsonToolResult(map[string]any{"configured": configured, "proposals": proposals, "executed": false, "configured_automatically": false}), nil
}
func validationReceiptReport(ctx context.Context, configured map[string]string) (ToolResponse, error) {
	session, _ := GetContextValues(ctx)
	store, _ := ctx.Value(workflowStoreKey{}).(*workflow.Store)
	if store == nil {
		return jsonToolResult(map[string]any{"configured": configured, "available": false, "message": "task receipt storage is unavailable"}), nil
	}
	task, err := store.Get(ctx, session)
	if err != nil {
		return ToolResponse{}, err
	}
	fileErr := VerifyFileReceipts(ctx, task.FileChanges)
	if fileErr != nil {
		if err := store.Mutated(ctx, session); err != nil {
			return ToolResponse{}, err
		}
	}
	var workspaceErr error
	if task.NeedsWorkspaceVerification(configured) || fileErr != nil {
		var digest string
		digest, workspaceErr = WorkspaceDigest(ctx)
		if _, err := store.Observe(ctx, session, digest); err != nil {
			return ToolResponse{}, err
		}
	}
	task, err = store.Get(ctx, session)
	if err != nil {
		return ToolResponse{}, err
	}
	return jsonToolResult(map[string]any{"configured": configured, "task_revision": task.Revision, "task_digest": task.Digest, "receipts": task.Validation, "validation_problem": task.ValidationProblem(configured), "file_changes": task.FileChanges, "affected_files_verified": fileErr == nil && len(task.FileChanges) > 0, "file_verification_error": verificationError(fileErr), "workspace_verification_error": verificationError(workspaceErr), "file_verification_is_test_validation": false}), nil
}

func verificationError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type checkCandidate struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Source  string `json:"source"`
}

func discoverCheckCandidates(ctx context.Context) ([]checkCandidate, error) {
	proposals := []checkCandidate{}
	for _, manifest := range []string{"go.mod", "package.json", "pyproject.toml", "Cargo.toml", "Makefile"} {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		path := filepath.Join(config.WorkingDirectory(), manifest)
		f, err := openFile(ctx, path)
		if err != nil {
			continue
		}
		f.Close()
		raw, err := readFile(ctx, path)
		if err != nil {
			return nil, err
		}
		if len(raw) > 256<<10 {
			continue
		}
		proposals = append(proposals, manifestCheckCandidates(manifest, raw)...)
	}
	return proposals, nil
}
func manifestCheckCandidates(manifest string, raw []byte) []checkCandidate {
	switch manifest {
	case "go.mod":
		return []checkCandidate{{"test", "go test ./...", manifest}, {"vet", "go vet ./...", manifest}}
	case "Cargo.toml":
		return []checkCandidate{{"test", "cargo test", manifest}, {"check", "cargo check", manifest}}
	case "pyproject.toml":
		return []checkCandidate{{"test", "python -m pytest", manifest}}
	case "package.json":
		return nodeCheckCandidates(manifest, raw)
	case "Makefile":
		return makeCheckCandidates(manifest, raw)
	}
	return nil
}
func nodeCheckCandidates(manifest string, raw []byte) []checkCandidate {
	var pkg struct{ Scripts map[string]string }
	if json.Unmarshal(raw, &pkg) != nil {
		return nil
	}
	names := []string{}
	for name := range pkg.Scripts {
		switch name {
		case "test", "lint", "typecheck", "check":
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var result []checkCandidate
	for _, name := range names {
		result = append(result, checkCandidate{name, "npm run " + name, manifest})
	}
	return result
}
func makeCheckCandidates(manifest string, raw []byte) []checkCandidate {
	var result []checkCandidate
	for line := range strings.SplitSeq(string(raw), "\n") {
		for _, name := range []string{"test", "check", "lint"} {
			if strings.HasPrefix(line, name+":") {
				result = append(result, checkCandidate{name, "make " + name, manifest})
			}
		}
	}
	return result
}
