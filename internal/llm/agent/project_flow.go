package agent

import (
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

func flowResponseGuidance(run *generationRun, history []message.Message) string {
	for _, msg := range history {
		if msg.ID == run.turnMessageID && tools.ProjectFlowQuery(msg.Content().Text) != "" {
			return "Flow response: trace the requested entry, concrete implementation, downstream result and relevant failure/cancellation paths. Interfaces, signatures and dependency fields establish contracts, not execution behavior. Follow useful source calls using definition/references/call hierarchy tools when configured; textual matches are only candidates and LSP may be unavailable. Read the relevant implementation intervals before describing behavior. For a call or return claim, cite the executable call/return statement lines, not only a declaration header. Explain every input/result explicitly requested, preserving exact returned values and all requested branches when repairing references; a citation repair must retain the complete original answer rather than replace it with a subset. When specific inputs and results are requested, start their explanation with short, separate input → result pairs, with the relevant citation beside each pair; then explain the conditions in prose. Distinguish observed implementation, documented contract and inference; explicitly name missing links rather than claiming full coverage or resilience. Do not repeat verified reads merely to repair citations. "
		}
	}
	return ""
}
