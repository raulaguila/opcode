package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diff"
	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/permission"
	"sort"
)

type PatchParams struct {
	PatchText string `json:"patch_text"`
	Preview   bool   `json:"preview,omitempty"`
	PreviewID string `json:"preview_id,omitempty"`
}

type PatchResponseMetadata struct {
	FilesChanged []string          `json:"files_changed"`
	Additions    int               `json:"additions"`
	Removals     int               `json:"removals"`
	ReceiptID    string            `json:"receipt_id"`
	BeforeHashes map[string]string `json:"before_hashes"`
	AfterHashes  map[string]string `json:"after_hashes"`
}

type patchTool struct {
	lspClients  map[string]*lsp.Client
	permissions permission.Service
	files       history.Service
}

const (
	PatchToolName    = "patch"
	patchDescription = `Applies a patch to multiple files in one operation. This tool is useful for making coordinated changes across multiple files.

The patch text must follow this format:
*** Begin Patch
*** Update File: /path/to/file
@@ Context line (unique within the file)
 Line to keep
-Line to remove
+Line to add
 Line to keep
*** Add File: /path/to/new/file
+Content of the new file
+More content
*** Delete File: /path/to/file/to/delete
*** End Patch

Before using this tool:
1. Use the view tool to understand the files' contents and context
2. Verify all file paths are correct (use the ls tool)

CRITICAL REQUIREMENTS FOR USING THIS TOOL:

1. UNIQUENESS: Context lines MUST uniquely identify the specific sections you want to change
2. PRECISION: All whitespace, indentation, and surrounding code must match exactly
3. VALIDATION: Ensure edits result in idiomatic, correct code
4. PATHS: Always use absolute file paths (starting with /)

The tool will apply all changes in a single atomic operation.`
)

func NewPatchTool(lspClients map[string]*lsp.Client, permissions permission.Service, files history.Service) BaseTool {
	return &patchTool{
		lspClients:  lspClients,
		permissions: permissions,
		files:       files,
	}
}

func (p *patchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        PatchToolName,
		Description: patchDescription,
		Parameters: map[string]any{
			"preview":    map[string]any{"type": "boolean", "description": "Prepare a bounded diff and preview_id without changing files or requesting edit approval."},
			"preview_id": map[string]any{"type": "string", "description": "Optional identifier from preview. Applying rejects changed source or patch; approval is still required."},
			"patch_text": map[string]any{
				"type":        "string",
				"description": "The full patch text that describes all changes to be made",
			},
		},
		Required: []string{"patch_text"},
	}
}

func (p *patchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params PatchParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("invalid parameters"), nil
	}

	if params.PatchText == "" {
		return NewTextErrorResponse("patch_text is required"), nil
	}

	// Identify all files needed for the patch and verify they've been read
	filesToRead := diff.IdentifyFilesNeeded(params.PatchText)
	for _, filePath := range filesToRead {
		absPath := filePath
		if !filepath.IsAbs(absPath) {
			wd := config.WorkingDirectory()
			absPath = filepath.Join(wd, absPath)
		}

		if getLastReadTime(absPath).IsZero() {
			return NewTextErrorResponse(fmt.Sprintf("you must read the file %s before patching it. Use the view tool first", filePath)), nil
		}

		fileInfo, err := statFile(ctx, absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return NewTextErrorResponse(fmt.Sprintf("file not found: %s", absPath)), nil
			}
			return ToolResponse{}, fmt.Errorf("failed to access file: %w", err)
		}

		if fileInfo.IsDir() {
			return NewTextErrorResponse(fmt.Sprintf("path is a directory, not a file: %s", absPath)), nil
		}

		modTime := fileInfo.ModTime()
		lastRead := getLastReadTime(absPath)
		if modTime.After(lastRead) {
			return NewTextErrorResponse(
				fmt.Sprintf("file %s has been modified since it was last read (mod time: %s, last read: %s)",
					absPath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
				)), nil
		}
	}

	// Check for new files to ensure they don't already exist
	filesToAdd := diff.IdentifyFilesAdded(params.PatchText)
	for _, filePath := range filesToAdd {
		absPath := filePath
		if !filepath.IsAbs(absPath) {
			wd := config.WorkingDirectory()
			absPath = filepath.Join(wd, absPath)
		}

		_, err := statFile(ctx, absPath)
		if err == nil {
			return NewTextErrorResponse(fmt.Sprintf("file already exists and cannot be added: %s", absPath)), nil
		} else if !os.IsNotExist(err) {
			return ToolResponse{}, fmt.Errorf("failed to check file: %w", err)
		}
	}

	// Load all required files
	currentFiles := make(map[string]string)
	for _, filePath := range filesToRead {
		absPath := filePath
		if !filepath.IsAbs(absPath) {
			wd := config.WorkingDirectory()
			absPath = filepath.Join(wd, absPath)
		}

		content, err := readFile(ctx, absPath)
		if err != nil {
			return ToolResponse{}, fmt.Errorf("failed to read file %s: %w", absPath, err)
		}
		currentFiles[filePath] = string(content)
	}

	// Process the patch
	patch, fuzz, err := diff.TextToPatch(params.PatchText, currentFiles)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("failed to parse patch: %s", err)), nil
	}

	if fuzz > 3 {
		return NewTextErrorResponse(fmt.Sprintf("patch contains fuzzy matches (fuzz level: %d). Please make your context lines more precise", fuzz)), nil
	}

	// Convert patch to commit
	commit, err := diff.PatchToCommit(patch, currentFiles)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("failed to create commit from patch: %s", err)), nil
	}

	previewID, beforeHashes, afterHashes, preview := patchPreview(params.PatchText, &commit)
	if params.PreviewID != "" && params.PreviewID != previewID {
		return NewTextErrorResponse("patch or source changed since preview; create a new preview"), nil
	}
	if params.Preview {
		return jsonToolResult(map[string]any{"preview_id": previewID, "before_hashes": beforeHashes, "after_hashes": afterHashes, "diff": preview, "applied": false, "authorization": "preview grants no permission; applying requests normal approvals"}), nil
	}
	// Get session ID and message ID
	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a patch")
	}

	// Request permission for all changes
	for path, change := range commit.Changes {
		absPath := path
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(config.WorkingDirectory(), absPath)
		}
		absPath = filepath.Clean(absPath)
		if change.MovePath != nil {
			destination := *change.MovePath
			if !filepath.IsAbs(destination) {
				destination = filepath.Join(config.WorkingDirectory(), destination)
			}
			destination = filepath.Clean(destination)
			if destination == absPath {
				// Applying a move onto itself would write the file and then delete it.
				change.MovePath = nil
			} else {
				if _, err := statFile(ctx, destination); err == nil {
					return NewTextErrorResponse(fmt.Sprintf("move destination already exists: %s", destination)), nil
				} else if !os.IsNotExist(err) {
					return ToolResponse{}, fmt.Errorf("failed to check move destination: %w", err)
				}
				change.MovePath = &destination
			}
			commit.Changes[path] = change
		}
		switch change.Type {
		case diff.ActionAdd:
			dir := filepath.Dir(absPath)
			patchDiff, _, _ := diff.GenerateDiff("", *change.NewContent, path)
			allowed := p.permissions.Request(ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        dir,
					ToolName:    PatchToolName,
					Action:      "create",
					Description: fmt.Sprintf("Create file %s", path),
					Params: EditPermissionsParams{
						FilePath: path,
						Diff:     patchDiff,
					},
				},
			)
			if !allowed {
				return ToolResponse{}, permission.ErrorPermissionDenied
			}
		case diff.ActionUpdate:
			currentContent := ""
			if change.OldContent != nil {
				currentContent = *change.OldContent
			}
			newContent := ""
			if change.NewContent != nil {
				newContent = *change.NewContent
			}
			patchDiff, _, _ := diff.GenerateDiff(currentContent, newContent, path)
			dir := filepath.Dir(absPath)
			allowed := p.permissions.Request(ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        dir,
					ToolName:    PatchToolName,
					Action:      "update",
					Description: fmt.Sprintf("Update file %s", path),
					Params: EditPermissionsParams{
						FilePath: path,
						Diff:     patchDiff,
					},
				},
			)
			if !allowed {
				return ToolResponse{}, permission.ErrorPermissionDenied
			}
			if change.MovePath != nil {
				destination := *change.MovePath
				moveDiff, _, _ := diff.GenerateDiff("", newContent, destination)
				if !p.permissions.Request(ctx, permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        filepath.Dir(destination),
					ToolName:    PatchToolName,
					Action:      "create",
					Description: fmt.Sprintf("Move file %s to %s", path, destination),
					Params:      EditPermissionsParams{FilePath: destination, Diff: moveDiff},
				}) {
					return ToolResponse{}, permission.ErrorPermissionDenied
				}
			}
		case diff.ActionDelete:
			dir := filepath.Dir(absPath)
			patchDiff, _, _ := diff.GenerateDiff(*change.OldContent, "", path)
			allowed := p.permissions.Request(ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        dir,
					ToolName:    PatchToolName,
					Action:      "delete",
					Description: fmt.Sprintf("Delete file %s", path),
					Params: EditPermissionsParams{
						FilePath: path,
						Diff:     patchDiff,
					},
				},
			)
			if !allowed {
				return ToolResponse{}, permission.ErrorPermissionDenied
			}
		}
	}

	changes := map[string]fileChange{}
	put := func(path string, change fileChange) error {
		if _, exists := changes[path]; exists {
			return fmt.Errorf("patch contains conflicting destinations")
		}
		changes[path] = change
		return nil
	}
	for path, change := range commit.Changes {
		abs := absolutePath(path)
		switch change.Type {
		case diff.ActionAdd:
			if err := put(abs, fileChange{next: change.NewContent}); err != nil {
				return ToolResponse{}, err
			}
		case diff.ActionDelete:
			if err := put(abs, fileChange{old: change.OldContent}); err != nil {
				return ToolResponse{}, err
			}
		case diff.ActionUpdate:
			if change.MovePath != nil {
				destination := absolutePath(*change.MovePath)
				if _, exists := changes[destination]; exists {
					return NewTextErrorResponse("conflicting patch destinations"), nil
				}
				if err := put(destination, fileChange{next: change.NewContent}); err != nil {
					return ToolResponse{}, err
				}
				if err := put(abs, fileChange{old: change.OldContent}); err != nil {
					return ToolResponse{}, err
				}
			} else {
				if err := put(abs, fileChange{old: change.OldContent, next: change.NewContent}); err != nil {
					return ToolResponse{}, err
				}
			}
		}
	}
	if err = applyFiles(ctx, changes); err != nil {
		return ToolResponse{}, err
	}

	// Update file history for all modified files
	changedFiles := []string{}
	totalAdditions := 0
	totalRemovals := 0

	historyFailed := false
	for path, change := range changes {
		changedFiles = append(changedFiles, path)
		oldContent, newContent := "", ""
		if change.old != nil {
			oldContent = *change.old
		}
		if change.next != nil {
			newContent = *change.next
		}
		_, additions, removals := diff.GenerateDiff(oldContent, newContent, path)
		totalAdditions += additions
		totalRemovals += removals
		file, historyErr := p.files.GetByPathAndSession(ctx, path, sessionID)
		if historyErr != nil {
			file, historyErr = p.files.Create(ctx, sessionID, path, oldContent)
		}
		if historyErr == nil && file.Content != oldContent {
			_, historyErr = p.files.CreateVersion(ctx, sessionID, path, oldContent)
		}
		if historyErr == nil {
			_, historyErr = p.files.CreateVersion(ctx, sessionID, path, newContent)
		}
		historyFailed = historyFailed || historyErr != nil
		recordFileWrite(path)
		recordFileRead(path)
	}
	sort.Strings(changedFiles)

	// Run LSP diagnostics on all changed files
	for _, filePath := range changedFiles {
		waitForLspDiagnostics(ctx, filePath, p.lspClients)
	}

	result := fmt.Sprintf("Patch applied successfully. %d files changed, %d additions, %d removals",
		len(changedFiles), totalAdditions, totalRemovals)

	if historyFailed {
		result += "; filesystem updated but file history could not be fully saved"
	}
	diagnosticsText := ""
	for _, filePath := range changedFiles {
		diagnosticsText += getDiagnostics(filePath, p.lspClients)
	}

	if diagnosticsText != "" {
		result += "\n\nDiagnostics:\n" + diagnosticsText
	}

	response := WithResponseMetadata(
		NewTextResponse(result),
		PatchResponseMetadata{
			FilesChanged: changedFiles,
			ReceiptID:    previewID, BeforeHashes: beforeHashes, AfterHashes: afterHashes,
			Additions: totalAdditions,
			Removals:  totalRemovals,
		})
	response.IsError = historyFailed
	return response, nil
}

func patchPreview(patch string, commit *diff.Commit) (string, map[string]string, map[string]string, string) {
	before, after := map[string]string{}, map[string]string{}
	paths := []string{}
	for path := range commit.Changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var signature, preview string
	for _, path := range paths {
		change := commit.Changes[path]
		old, next := "", ""
		if change.OldContent != nil {
			old = *change.OldContent
		}
		if change.NewContent != nil {
			next = *change.NewContent
		}
		before[path] = indexDigest(old)
		after[path] = indexDigest(next)
		signature += path + before[path] + after[path]
		if change.MovePath != nil {
			signature += *change.MovePath
		}
		hunk, _, _ := diff.GenerateDiff(old, next, path)
		if len(preview) < 6000 {
			preview += clipString(redact(hunk), 6000-len(preview))
		}
	}
	return indexDigest(patch + signature), before, after, preview
}
