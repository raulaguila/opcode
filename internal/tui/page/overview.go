package page

import (
	message "github.com/raulaguila/opcode/internal/message"

	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/tui/commands"
	"strings"
)

// CommandPriorities changes discoverability, never authorization or task mode.
func (p *chatPage) CommandPriorities() []string {
	if p.FooterState().Busy {
		return []string{"terminal", "queue", "task", "cancel", "context"}
	}
	if p.recovery.err != nil && !p.recovery.resolved {
		return []string{"retry", "error", "models", "settings", "context", "changes"}
	}
	if p.workflow.task.Objective != "" {
		return []string{"task", "changes", "checks", "resume", "context"}
	}
	return []string{"new", "models", "files", "settings", "task"}
}
func (p *chatPage) taskBody() string {
	body := strings.Join(p.planRows(), "\n")
	if p.workflow.task.Objective == "" {
		body = config.Text("Descreva o resultado desejado na conversa. O plano e os critérios aparecerão aqui.", "Describe the desired result in the conversation. The plan and criteria will appear here.")
	}
	body += "\n\n" + p.activityLabel() + p.queueSummary()
	if p.workflow.question.ID != "" {
		body += "\n" + config.Text("Volte à conversa para responder à pergunta pendente.", "Return to the conversation to answer the pending question.")
	}

	return body
}
func (p *chatPage) openTask() tea.Cmd {
	p.refreshWorkflow()
	p.showWorkspace("task", config.Text("Tarefa", "Task"), p.taskBody())
	p.workspace.actions = p.taskActions()
	return nil
}
func (p *chatPage) taskActions() []workspaceAction {
	var actions []workspaceAction
	if p.workflow.task.Objective != "" {
		actions = append(actions, workspaceAction{kind: "adjust-plan", label: config.Text("Ajustar plano", "Adjust plan")}, workspaceAction{kind: "execute-plan", label: config.Text("Executar com revisão…", "Execute with review…")})
	}

	for _, id := range []string{"contract", "queue", "changes", "checks", "permissions", "context", "terminal"} {
		for _, entry := range commands.Builtins {
			if entry.ID == id {
				title, _ := commands.Presentation(id, entry.Title, entry.Description)
				actions = append(actions, workspaceAction{kind: "command", text: id, label: title})
			}
		}
	}
	return actions
}
func contextPanel(kind string) bool {
	return kind == "context" || kind == "runtime" || kind == "index" || kind == "local-status"
}
func (p *chatPage) contextTab(key string) tea.Cmd {
	switch key {
	case "1":
		return p.openContext(false)
	case "2":
		return p.openIndex("")
	case "3":
		return p.openLocalStatus()
	case "4":
		return p.openRuntime()
	}
	return nil
}

func (p *chatPage) contextReceipts(ctx context.Context, id string) string {
	if p.app.Messages == nil {
		return ""
	}
	messages, err := p.app.Messages.List(ctx, id)
	if err != nil {
		return ""
	}
	lines := []string{}
	seen := map[string]bool{}
	contextReceiptsMessages(messages, &lines, seen)
	if len(lines) == 0 {
		return ""
	}
	return "\n\n" + config.Text("Leituras recentes · até 12 intervalos (histórico; não garante inclusão na próxima chamada):", "Recent reads · up to 12 ranges (history; inclusion in the next request is not guaranteed):") + "\n" + strings.Join(lines, "\n")
}

func contextReceiptsMessages(messages []message.Message, lines *[]string, seen map[string]bool) {
	for i := len(messages) - 1; i >= 0 && len((*lines)) < 12; i-- {
		for _, result := range messages[i].ToolResults() {
			if result.IsError {
				continue
			}
			var meta struct {
				SourceRead *tools.SourceReadReceipt `json:"source_read"`
			}
			if json.Unmarshal([]byte(result.Metadata), &meta) != nil || meta.SourceRead == nil {
				continue
			}
			r := meta.SourceRead
			label := fmt.Sprintf("%s:%d-%d", r.Path, r.StartLine, r.EndLine)
			if !seen[label] && len((*lines)) < 12 {
				seen[label] = true
				(*lines) = append((*lines), panelText(label))
			}
		}
	}
}
