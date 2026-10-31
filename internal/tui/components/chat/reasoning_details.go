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
	thinking := item.ReasoningContent().Thinking
	if thinking == "" {
		return
	}
	key := r.groupKey + "\x00process:reasoning:" + item.ID
	expanded := r.processExpanded(turnGroup{key: key}, false)
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
