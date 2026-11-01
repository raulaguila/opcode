package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
)

func processSummary(messages []message.Message) string {
	// A few direct tool calls read better by name and target than as a count.
	var briefs []string
	direct := true
	for _, msg := range messages {
		direct = direct && !msg.IsApplication()
		for _, call := range msg.ToolCalls() {
			direct = direct && !strings.HasPrefix(call.Origin, "context")
			briefs = append(briefs, toolBrief(call))
		}
	}
	if direct && len(briefs) > 0 && len(briefs) <= 3 {
		return strings.Join(briefs, " · ")
	}
	var reads, searches, reused, commands, calls, repairs int
	processSummaryMessages(messages, &reads, &searches, &reused, &commands, &calls, &repairs)
	var labels []string
	for _, item := range []struct {
		n                    int
		pt, en, onePT, oneEN string
	}{
		{reads, "leituras de contexto", "context reads", "leitura de contexto", "context read"},
		{searches, "buscas de contexto", "context searches", "busca de contexto", "context search"},
		{reused, "fontes reutilizadas", "reused sources", "fonte reutilizada", "reused source"},
		{commands, "chamadas de terminal", "terminal calls", "chamada de terminal", "terminal call"},
		{calls, "ferramentas", "tool calls", "ferramenta", "tool call"},
		{repairs, "reparos", "repairs", "reparo", "repair"},
	} {
		if item.n > 0 {
			if item.n == 1 {
				item.pt, item.en = item.onePT, item.oneEN
			}
			labels = append(labels, fmt.Sprintf("%d %s", item.n, config.Text(item.pt, item.en)))
		}
	}
	if len(labels) == 0 {
		return config.Text("raciocínio", "reasoning")
	}
	return strings.Join(labels, " · ")
}

func processSummaryMessages(messages []message.Message, reads *int, searches *int, reused *int, commands *int, calls *int, repairs *int) {
	for _, msg := range messages {
		if msg.IsApplication() {
			(*repairs)++
		}
		for _, call := range msg.ToolCalls() {
			switch {
			case call.Origin == "context-reused":
				(*reused)++
			case call.Origin == "context" && call.Name == "view":
				(*reads)++
			case call.Origin == "context" && call.Name == "search_code_index":
				(*searches)++
			case call.Name == "bash" || call.Name == "process_start" || call.Name == "run_checks":
				(*commands)++
			default:
				(*calls)++
			}
		}
	}
}

func completedReasoning(messages []message.Message) bool {
	found := false
	for _, item := range messages {
		if item.Role != message.Assistant || len(item.ToolCalls()) > 0 || item.IsApplication() {
			return false
		}
		if item.ReasoningContent().Thinking != "" {
			found = true
			if !item.IsFinished() {
				return false
			}
		}
	}
	return found
}

func reasoningOnly(messages []message.Message) bool {
	found := false
	for _, item := range messages {
		if item.Role != message.Assistant || len(item.ToolCalls()) > 0 || item.IsApplication() {
			return false
		}
		found = found || item.ReasoningContent().Thinking != ""
	}
	return found
}

// toolBrief names a call by its tool and main argument, e.g. "edit store.go".
func toolBrief(call message.ToolCall) string {
	var input map[string]any
	_ = json.Unmarshal([]byte(call.Input), &input)
	for _, field := range []string{"file_path", "path", "command", "pattern", "query", "url", "name"} {
		value, ok := input[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		if field == "file_path" || field == "path" {
			value = filepath.Base(value)
		}
		value = strings.Join(strings.Fields(value), " ")
		if len([]rune(value)) > 40 {
			value = string([]rune(value)[:39]) + "…"
		}
		return call.Name + " " + value
	}
	return call.Name
}
