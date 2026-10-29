package agent

import (
	"strings"
	"unicode"

	"github.com/raulaguila/opcode/internal/llm/tools"
)

func localConversationTitle(content string) string {
	text := strings.Join(strings.Fields(content), " ")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	text = tools.RedactCredentials(text)
	runes := []rune(text)
	if len(runes) > 72 {
		text = string(runes[:71]) + "…"
	}
	return text
}
