package chat

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
)

const loadingPreviewBytes = 4 << 10
const loadingPreviewLines = 64

// A preview owns text only. It never enters history, search, selection, actions,
// or the authoritative reload; a quiet read still reconciles the entire session.
type loadingPreview struct{ id, text string }

func (m *messagesCmp) updateLoadingPreview(event pubsub.Event[message.Message]) {
	if event.ReconcileRequired {
		m.loadingPreview = loadingPreview{}
		return
	}
	item := event.Payload
	if item.SessionID != m.session.ID {
		return
	}
	if event.Type == pubsub.DeletedEvent {
		if item.ID == m.loadingPreview.id {
			m.loadingPreview = loadingPreview{}
		}
		return
	}
	if event.Type != pubsub.CreatedEvent && event.Type != pubsub.UpdatedEvent {
		return
	}
	if item.Role != message.Assistant || item.Content().Origin == "application" || len(item.ToolCalls()) > 0 {
		if item.ID == m.loadingPreview.id {
			m.loadingPreview = loadingPreview{}
		}
		return
	}
	text := boundedLoadingText(item.Content().String())
	if text == "" {
		m.loadingPreview = loadingPreview{}
		return
	}
	m.loadingPreview = loadingPreview{id: item.ID, text: text}
}

func boundedLoadingText(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(tools.RedactCredentials(text)))
	start := max(0, len(text)-loadingPreviewBytes)
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	text = text[start:]
	lines := 0
	for index := len(text) - 1; index >= 0; index-- {
		if text[index] == '\n' {
			lines++
			if lines >= loadingPreviewLines {
				text = text[index+1:]
				break
			}
		}
	}
	return strings.Clone(text)
}

func (m *messagesCmp) loadingView() string {
	label := config.Text("Carregando conversa…", "Loading conversation…")
	if m.loadingPreview.text == "" || m.width < 1 {
		return label
	}
	window := newTranscriptViewport(m.width, max(0, m.viewport.Height-1))
	window.SetContent(renderMessage(m.loadingPreview.text, false, false, m.width))
	window.GotoBottom()
	return lipgloss.JoinVertical(lipgloss.Left, label, window.View())
}
