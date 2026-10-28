package transcript

import (
	"fmt"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
)

// Markdown includes visible conversation and tool process, never provider
// continuation state or hidden reasoning. The caller supplies credential masking.
func Markdown(s session.Session, messages []message.Message, redact func(string) string) ([]byte, error) {
	if len(messages) > 5000 {
		return nil, fmt.Errorf("conversation exceeds export limit (5000 messages)")
	}
	if redact == nil {
		redact = func(s string) string { return s }
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\nSessão: `%s`\nExportada em: %s\n\n", strings.ReplaceAll(s.Title, "\n", " "), s.ID, time.Now().Format(time.RFC3339))
	for _, msg := range messages {
		switch msg.Role {
		case message.User:
			out.WriteString("## Você\n\n")
		case message.Assistant:
			out.WriteString("## Assistente\n\n")
		case message.Tool:
			out.WriteString("## Resultado de ferramenta\n\n")
		default:
			continue
		}
		out.WriteString(redact(msg.Content().String()))
		out.WriteString("\n\n")
		for _, call := range msg.ToolCalls() {
			fmt.Fprintf(&out, "### Ferramenta: %s\n\n%s\n\n", redact(call.Name), fenced(redact(call.Input)))
		}
		for _, result := range msg.ToolResults() {
			fmt.Fprintf(&out, "### %s (erro: %t)\n\n%s\n\n", redact(result.Name), result.IsError, fenced(redact(result.Content)))
		}
		if out.Len() > 4<<20 {
			return nil, fmt.Errorf("conversation exceeds export limit (4 MiB)")
		}
	}
	return []byte(redact(out.String())), nil
}
func fenced(text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "text\n" + text + "\n" + fence
}
