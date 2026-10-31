package agent

import (
	"iter"
	"strconv"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

func (b *evidenceBootstrap) prepareTopic(query string, index, inventory tools.BaseTool) (bool, error) {
	query, err := b.topicQuery(query, inventory)
	if err != nil {
		return false, err
	}
	read, err := b.readIndex(query, index)
	if err == nil && b.flow && read {
		err = b.followFlowCalls(index)
	}
	return read, err
}

// Try at most three distinct clues until new source is read; LSP stays explicit.
func (b *evidenceBootstrap) followFlowCalls(index tools.BaseTool) error {
	if b.flowReadsRemaining == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, msg := range b.history[sourceReadTurnStart(b.run, b.history):] {
		for _, result := range msg.ToolResults() {
			stop, err := b.followFlowSource(result, index, seen)
			if stop || err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *evidenceBootstrap) followFlowSource(result message.ToolResult, index tools.BaseTool, seen map[string]bool) (bool, error) {
	if result.IsError {
		return false, nil
	}
	if _, ok := tools.VisibleSourceReadReceipt(result.Name, result.Content, result.Metadata); !ok {
		return false, nil
	}
	for name := range flowCallHints(result.Content) {
		if seen[name] {
			continue
		}
		seen[name] = true
		read, err := b.readIndex(name, index)
		if err != nil || read || len(seen) == 3 || b.flowReadsRemaining == 0 {
			return true, err
		}
	}
	return false, nil
}

func flowCallHints(content string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for line := range strings.SplitSeq(content, "\n") {
			number, source, ok := strings.Cut(line, "|")
			if _, err := strconv.Atoi(strings.TrimSpace(number)); !ok || err != nil {
				continue
			}
			source = flowExecutableLine(source)
			for _, match := range entryCalls.FindAllStringSubmatch(source, 8) {
				if !skipEntryCall(match[1], match[2]) && !yield(match[2]) {
					return
				}
			}
		}
	}
}

func flowExecutableLine(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/*") {
		return ""
	}
	for _, prefix := range []string{"func ", "def ", "async def ", "function ", "export function ", "export async function "} {
		if strings.HasPrefix(line, prefix) {
			_, body, _ := strings.Cut(line, "{")
			return body
		}
	}
	return line
}

func flowResponseGuidance(run *generationRun, history []message.Message) string {
	for _, msg := range history {
		if msg.ID == run.turnMessageID && tools.ProjectFlowQuery(msg.Content().Text) != "" {
			return "Flow response: trace the requested entry, concrete implementation, downstream result and relevant failure/cancellation paths. Interfaces, signatures and dependency fields establish contracts, not execution behavior. Follow useful source calls using definition/references/call hierarchy tools when configured; textual matches are only candidates and LSP may be unavailable. Read the relevant implementation intervals before describing behavior. For a call or return claim, cite the executable call/return statement lines, not only a declaration header. Explain every input/result explicitly requested, preserving exact returned values and all requested branches when repairing references; a citation repair must retain the complete original answer rather than replace it with a subset. When specific inputs and results are requested, start their explanation with short, separate input → result pairs, with the relevant citation beside each pair; then explain the conditions in prose. Distinguish observed implementation, documented contract and inference; explicitly name missing links rather than claiming full coverage or resilience. Do not repeat verified reads merely to repair citations. "
		}
	}
	return ""
}
