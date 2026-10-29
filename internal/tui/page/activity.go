package page

import (
	"fmt"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"strings"
)

type ActivityMsg struct{ SessionID, State string }

func ActivityForMessage(m message.Message) string {
	if m.IsApplication() {
		return config.Text("Conferindo resposta e fontes", "Checking answer and sources")
	}
	calls := m.ToolCalls()
	if len(calls) == 0 {
		if m.Content().Text != "" {
			return config.Text("Preparando resposta", "Writing response")
		}
		return config.Text("Consultando modelo", "Contacting model")
	}
	name := calls[len(calls)-1].Name
	switch {
	case strings.Contains(name, "check") || strings.Contains(name, "validation"):
		return config.Text("Verificando resultado", "Checking results")
	case name == "bash" || strings.Contains(name, "process"):
		return config.Text("Executando comando", "Running command")
	case strings.Contains(name, "write") || strings.Contains(name, "edit") || strings.Contains(name, "patch"):
		return config.Text("Alterando arquivos", "Editing files")
	default:
		return config.Text("Investigando", "Investigating")
	}
}
func (p *chatPage) activityLabel() string {
	if p.workflow.question.ID != "" {
		return config.Text("Aguardando sua resposta", "Waiting for your answer")
	}
	if p.progress != "" {
		return p.progress
	}

	switch p.outcome {
	case "blocked":
		return config.Text("Tarefa bloqueada", "Task blocked")
	case "partial":
		return config.Text("Trabalho parcial", "Partial work")
	case "planned":
		return config.Text("Plano preparado", "Plan prepared")
	case "completed":
		return config.Text("Concluído", "Completed")
	}
	return config.Text("Pronto", "Ready")
}
func (p *chatPage) pendingLabel(n int) string {
	return fmt.Sprintf(config.Text(" · %d pendente(s) · /queue", " · %d pending · /queue"), n)
}
