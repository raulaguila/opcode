package page

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
)

type recoveryAction struct{ command, label string }
type recoveryActionSpan struct {
	command         string
	row, start, end int
}

func recoveryActionOptions(err error, changed bool) []recoveryAction {
	actions := recoveryKindActions(err)
	if changed {
		// A stopped run can leave half-applied edits; reverting is reviewed first.
		actions = append([]recoveryAction{{"undo", config.Text("Desfazer alterações", "Undo changes")}}, actions...)
	}
	return actions
}

func recoveryKindActions(err error) []recoveryAction {
	kind := recoveryInterrupted
	if err != nil {
		kind = classifyRecovery(err)
	}
	details := recoveryAction{"error", config.Text("Detalhes", "Details")}
	models := recoveryAction{"models", config.Text("Trocar modelo", "Change model")}
	switch kind {
	case recoveryOutput:
		return []recoveryAction{{"settings", config.Text("Rever tokens de saída", "Review output tokens")}, details}
	case recoveryCitation:
		return []recoveryAction{{"error", config.Text("Ver detalhes", "View details")}, {"retry", config.Text("Solicitar correção", "Request correction")}}
	case recoveryBudget:
		return []recoveryAction{{"task", config.Text("Rever tarefa", "Review task")}, details}
	case recoveryAuthentication:
		return []recoveryAction{{"settings", config.Text("Configurar acesso", "Configure access")}, details}
	case recoveryCapacity:
		return []recoveryAction{{"context", config.Text("Rever contexto", "Review context")}, models, details}
	case recoveryMemory:
		return []recoveryAction{{"models", config.Text("Escolher modelo menor", "Choose smaller model")}, {"local-status", config.Text("Alocação", "Allocation")}, details}
	case recoveryUnavailable:
		return []recoveryAction{{"retry", config.Text("Tentar novamente", "Try again")}, models, details}
	case recoveryConflict:
		return []recoveryAction{{"changes", config.Text("Revisar arquivos", "Review files")}, details}
	default:
		return []recoveryAction{{"retry", config.Text("Retomar", "Resume")}, details, {"settings", config.Text("Configuração", "Configuration")}}
	}
}

func recoveryActionText(action recoveryAction, compact bool) string {
	text := "/" + action.command
	if !compact && action.label != "" {
		text += " " + action.label
	}
	return text
}

// Render and click use the same visible text and cell ranges.
func layoutRecoveryActions(err error, width int, changed bool) ([]string, []recoveryActionSpan) {
	if width < 20 {
		return nil, nil
	}
	actions := recoveryActionOptions(err, changed)
	full := make([]string, len(actions))
	for i, action := range actions {
		full[i] = recoveryActionText(action, false)
	}
	compact := ansi.StringWidth(" "+strings.Join(full, " · ")) > width
	var rows []string
	var spans []recoveryActionSpan
	line := " "
	for _, action := range actions {
		text := recoveryActionText(action, compact)
		separator := ""
		if line != " " {
			separator = " · "
		}
		if ansi.StringWidth(line+separator+text) > width {
			rows = append(rows, line)
			line, separator = " ", ""
		}
		line += separator
		start := ansi.StringWidth(line)
		line += text
		spans = append(spans, recoveryActionSpan{action.command, len(rows), start, ansi.StringWidth(line)})
	}
	return append(rows, line), spans
}
