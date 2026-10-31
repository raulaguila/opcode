package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
)

// Load only the selected conversation, off the UI thread. The dialog ignores
// results for an older selection; no model or tool is invoked for a preview.
func (a *appModel) sessionPreview(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		items, err := a.app.Messages.List(ctx, id)
		preview := config.Text("Nenhuma mensagem ainda", "No messages yet")
		sessionPreviewState(items, err, &preview)
		return dialog.SessionPreviewLoadedMsg{ID: id, Text: preview}
	}
}

func sessionPreviewState(items []message.Message, err error, preview *string) {
	if err != nil {
		(*preview) = config.Text("Prévia indisponível; Enter abre a conversa", "Preview unavailable; Enter opens the conversation")
	} else {
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].Role != message.User || items[i].IsApplication() {
				continue
			}
			text := strings.Join(strings.Fields(ansi.Strip(tools.RedactCredentials(items[i].Content().Text))), " ")
			if text == "" {
				continue
			}
			runes := []rune(text)
			if len(runes) > 240 {
				text = string(runes[:240]) + "…"
			}
			(*preview) = config.Text("Última mensagem: ", "Last message: ") + text
			break
		}
	}
}
