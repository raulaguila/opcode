package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/workflow"
)

var errWorkspaceVerification = errors.New("workspace verification unavailable")

func fileContentHash(content *string) string {
	if content == nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(*content)))
}

func (a *agent) recordFileChanges(ctx context.Context, session string, changes []tools.FileChange) error {
	files := make([]workflow.FileReceipt, 0, len(changes))
	for _, change := range changes {
		files = append(files, workflow.FileReceipt{Path: change.Path, Before: fileContentHash(change.Before), After: fileContentHash(change.After)})
	}
	return a.workflow.RecordFileChanges(ctx, session, files)
}

func unscopedTool(tool tools.BaseTool) bool {
	if tools.IsOpaqueTool(tool) {
		return true
	}
	switch tool.Info().Name {
	case tools.WriteToolName, tools.EditToolName, tools.PatchToolName, "update_plan", "finish_task", "run_checks":
		return false
	}
	return !readOnlyTool(tool.Info().Name)
}

func (a *agent) requireWorkspace(ctx context.Context, session string) error {
	if a.workflow == nil {
		return nil
	}
	return a.workflow.RequireWorkspace(ctx, session)
}

func (a *agent) observeCompletion(ctx context.Context, session string) error {
	task, err := a.workflow.Get(ctx, session)
	if err != nil {
		return err
	}
	if err := tools.VerifyFileReceipts(ctx, task.FileChanges); err != nil {
		// A failed file check must not leave a previous test receipt current.
		return errors.Join(err, a.workflow.Mutated(ctx, session))
	}
	if task.NeedsWorkspaceVerification(checkCommands()) {
		if err := a.observeWorkspace(ctx, session); err != nil {
			return fmt.Errorf("%w: %w", errWorkspaceVerification, err)
		}
	}
	return nil
}
