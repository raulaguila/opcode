package chat

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/styles"
)

// Reasoning belongs to its response, including folded citation revisions.
func (r *turnRenderer) renderReasoningDetails(item message.Message) {
	thinking := modelReasoning(item)
	if thinking == "" {
		return
	}
	key := r.groupKey + "\x00process:reasoning:" + item.ID
	expanded := r.reasoningExpanded(key)
	mark := "›"
	if expanded {
		mark = "⌄"
	}
	label := mark + " " + config.Text("Raciocínio do modelo", "Model reasoning")
	start := r.position
	r.appendText(key+":process", styles.HintOn(r.theme.Background()).Render(ansi.Truncate(label, r.messages.width, "…")))
	r.hits = append(r.hits, transcriptHit{start: start, end: start + 1, kind: "process", id: key, expanded: expanded})
	if expanded {
		header := config.Text("Raciocínio interno", "Internal reasoning")
		r.appendText(item.ID+":thinking", r.base.Foreground(r.theme.TextMuted()).Render(header+"\n"+toMarkdown(tools.RedactCredentials(thinking), false, r.messages.width)))
	}
}

func modelReasoning(item message.Message) string {
	if item.Role != message.Assistant || item.IsApplication() {
		return ""
	}
	return item.ReasoningContent().Thinking
}

func (r *turnRenderer) reasoningExpanded(key string) bool {
	m := r.messages
	if m.toggled[key] {
		return m.expanded[key]
	}
	if r.active {
		return true
	}
	// Scrolling away from the live tail is a reading choice, not consent to fold.
	if !m.viewport.AtBottom() && m.renderedProcessExpanded(key) {
		if m.expanded == nil {
			m.expanded = map[string]bool{}
			m.toggled = map[string]bool{}
		}
		m.expanded[key], m.toggled[key] = true, true
		return true
	}
	return false
}
