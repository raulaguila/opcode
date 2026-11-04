package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (a appModel) footerState() util.FooterState {
	if current, ok := a.pages[a.currentPage].(interface{ FooterState() util.FooterState }); ok {
		return current.FooterState()
	}
	return util.FooterState{}
}

func (a appModel) statusView() string { return a.status.ViewFor(a.footerState()) }

func (a *appModel) footerClick(x int) tea.Cmd {
	switch action := a.status.ActionAt(x, a.footerState()); action {
	case "models":
		a.showModelDialog = true
		return a.openModelCatalog("", false)
	case "approval":
		return a.openModeChooserAt(true)
	case "mode", "commands":
		if action == "mode" {
			return a.openModeChooser()
		}
		items := a.contextualCommands()
		a.commandDialog.SetCommands(items)
		a.showCommandDialog = true
	case "queue", "context", "error", "settings", "integrations":
		return util.CmdHandler(page.WorkspaceCommandMsg{Command: action})
	}
	return nil
}

func (a *appModel) openModeChooser() tea.Cmd {
	return a.openModeChooserAt(false)
}

func (a *appModel) openModeChooserAt(approvalFocus bool) tea.Cmd {
	var items []dialog.Command
	for _, command := range a.commands {
		if strings.HasPrefix(command.ID, "mode-") {
			items = append(items, command)
		}
	}
	for _, policy := range []permission.ApprovalPolicy{permission.ApprovalPrompt, permission.ApprovalSandboxed} {
		items = append(items, dialog.Command{ID: "approval-" + string(policy), Handler: func(dialog.Command) tea.Cmd {
			return util.CmdHandler(page.ApprovalPolicyMsg{Policy: policy})
		}})
	}
	a.commandDialog.SetCommands(items)
	if contextual, ok := a.commandDialog.(interface{ SetContext(string, string) }); ok {
		contextual.SetContext(config.Text("Modo de trabalho", "Work mode"), "mode-"+a.footerState().Mode)
	}
	policy := a.footerState().ApprovalPolicy
	if policy == "" {
		policy = string(permission.ApprovalPrompt)
	}
	if contextual, ok := a.commandDialog.(interface{ SetApprovalContext(string) }); ok {
		contextual.SetApprovalContext("approval-" + policy)
	}
	if approvalFocus {
		if contextual, ok := a.commandDialog.(interface{ FocusCommand(string) }); ok {
			contextual.FocusCommand("approval-" + policy)
		}
	}
	a.showCommandDialog = true
	return nil
}

func (a *appModel) contextualCommands() []dialog.Command {
	items := slices.Clone(a.commands)
	if p, ok := a.pages[page.ChatPage].(interface{ CommandPriorities() []string }); ok {
		priorities := p.CommandPriorities()
		rank := func(id string) int {
			n := slices.Index(priorities, id)
			if n < 0 {
				return len(priorities)
			}
			return n
		}
		slices.SortStableFunc(items, func(a, b dialog.Command) int { return rank(a.ID) - rank(b.ID) })
	}
	return items
}
