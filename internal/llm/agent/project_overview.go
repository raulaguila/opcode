package agent

import (
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

func overviewResponseGuidance(run *generationRun, history []message.Message) string {
	for _, msg := range history {
		if msg.ID == run.turnMessageID && tools.ProjectOverview(msg.Content().Text) {
			return "Overview response: name the project from its documentation and explain its purpose, practical capabilities and a few main components, not a tour of helper methods. Dependency names do not prove supported integrations/features; mention them only as declared dependencies if requested. Avoid unsupported quality judgments such as robust or production-ready. Keep these requested facts when repairing citations. End with one explicit sentence naming the inspected areas and an actual evaluation limit (for example, tests not run, if true). Put precise verified citations beside claims; do not add a reference-only footer. This orientation is based on a bounded inspection, never a complete audit. "
		}
	}
	return ""
}
