package commands

import (
	"github.com/raulaguila/opcode/internal/config"
	"strings"
)

var english = map[string][2]string{
	"cancel":  {"Stop activity", "Request cancellation while preserving progress"},
	"index":   {"Project index", "Status, update, rebuild, pause or clear derived index"},
	"runtime": {"Runtime diagnostic", "Model, protocol, context, tools and index; credentials omitted"},
	"new":     {"New conversation", "Start another conversation"}, "files": {"Files", "Choose files to attach"}, "terminal": {"Terminal", "Open or return to the terminal"}, "commands": {"All commands", "Search actions and tools"},
	"mode": {"Work mode", "Choose work mode…"}, "plan": {"Show/hide plan", "Show or collapse the plan"}, "task": {"Current task", "Inspect task and acceptance criteria"}, "resume": {"Resume task", "Continue the saved task"},
	"setup": {"Guided setup", "Provider, model and optional tests"}, "local-status": {"Ollama allocation", "Loaded context, CPU/GPU and model memory"}, "contract": {"Expected outcome", "Explain, plan, change or verify; targets and criteria"},
	"settings": {"Settings", "Providers and preferences"}, "models": {"Choose model", "Catalog, favorites and details"}, "integrations": {"Integrations", "Configure/test MCP or LSP"}, "sessions": {"Conversations", "Search, rename, archive and export"},
	"queue": {"Pending instructions", "Edit or remove queued messages"}, "context": {"Model context", "Estimated budget and reserves"}, "recalibrate": {"Recalibrate context", "Clear the learned limit for the current model"}, "compact": {"Compact context", "Prepare history for the next request"},
	"worktrees": {"Project and worktrees", "Branch, files, differences and isolated projects"}, "changes": {"Changes and checks", "Review files changed by each turn"}, "checks": {"Task checks", "Inspect current revision receipts"}, "undo": {"Undo turn", "Review reversal before applying"}, "redo": {"Redo turn", "Review reapplication before approval"},
	"permissions": {"Session permissions", "Inspect and revoke approvals"}, "skills": {"Available skills", "Inspect explicitly configured skills"}, "skill": {"Use a skill", "Enter its name and arguments"}, "actions": {"Response actions", "Copy response/code or open a citation"}, "export": {"Export conversation", "Save conversation and process as Markdown"}, "retry": {"Resume last request", "Inspect progress before continuing"}, "error": {"Error details", "Inspect execution diagnostics"}, "process": {"Show/hide process", "Review the latest turn's steps"}, "latest": {"Latest messages", "Resume following the response"},
}

func Localized(c Command) Command {
	if pair, ok := english[strings.TrimSpace(c.ID)]; ok {
		c.Title = config.Text(c.Title, pair[0])
		c.Description = config.Text(c.Description, pair[1])
	}
	return c
}

// Presentation localizes only known built-in labels, never custom command text.
func Presentation(id, title, description string) (string, string) {
	for _, entry := range Builtins {
		if strings.TrimSpace(entry.ID) == id && entry.Title == title && entry.Description == description {
			entry = Localized(entry)
			return entry.Title, entry.Description
		}
	}
	return title, description
}
