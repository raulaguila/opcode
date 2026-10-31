package agent

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

var entryCalls = regexp.MustCompile(`\b(?:([A-Za-z_]\w*)(?:\.|::))?([A-Za-z_]\w*)\s*\(`)

// Entry calls are navigation hints from delivered source, never read evidence.
func overviewQuery(history []message.Message) string {
	for _, msg := range slices.Backward(history) {
		for _, result := range msg.ToolResults() {
			receipt, ok := tools.VisibleSourceReadReceipt(result.Name, result.Content, result.Metadata)
			if result.IsError || !ok || !overviewEntryPoint(receipt.Path) {
				continue
			}
			if calls := overviewEntryCalls(result.Content); len(calls) > 0 {
				return strings.Join(calls, " ")
			}
		}
	}
	return "service server application"
}

func overviewEntryPoint(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "main.go", "main.py", "__main__.py", "main.rs", "main.ts", "main.js", "index.ts", "index.js", "program.cs":
		return true
	}
	return false
}

func overviewResponseGuidance(run *generationRun, history []message.Message) string {
	for _, msg := range history {
		if msg.ID == run.turnMessageID && tools.ProjectOverview(msg.Content().Text) {
			return "Overview response: name the project from its documentation and explain its purpose, practical capabilities and a few main components, not a tour of helper methods. Dependency names do not prove supported integrations/features; mention them only as declared dependencies if requested. Avoid unsupported quality judgments such as robust or production-ready. Keep these requested facts when repairing citations. End with one explicit sentence naming the inspected areas and an actual evaluation limit (for example, tests not run, if true). Put precise verified citations beside claims; do not add a reference-only footer. This orientation is based on a bounded inspection, never a complete audit. "
		}
	}
	return ""
}

func overviewSourceLimit(path, question string) int {
	switch filepath.Base(path) {
	case "go.mod", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml":
		for _, term := range []string{"depend", "bibliotec", "librar", "sdk", "stack"} {
			if strings.Contains(strings.ToLower(question), term) {
				return 80
			}
		}
		return 6 // Language/project header, rather than a distracting SDK inventory.
	}
	return 80
}

func overviewEntryCalls(content string) []string {
	var names []string
	for line := range strings.SplitSeq(content, "\n") {
		number, source, ok := strings.Cut(line, "|")
		if _, err := strconv.Atoi(strings.TrimSpace(number)); !ok || err != nil {
			continue
		}
		for _, match := range entryCalls.FindAllStringSubmatch(source, 8) {
			if skipEntryCall(match[1], match[2]) || slices.Contains(names, match[2]) {
				continue
			}
			names = append(names, match[2])
			if len(names) == 2 {
				return names
			}
		}
	}
	return names
}

func skipEntryCall(receiver, name string) bool {
	switch strings.ToLower(receiver) {
	case "fmt", "log", "logging", "console", "os", "sys", "errors", "time":
		return true
	}
	switch name {
	case "main", "func", "function", "if", "for", "while", "switch", "catch", "print", "println", "printf", "len", "make", "panic", "require":
		return true
	}
	return len(name) > 80
}

func overviewReadPaths(run *generationRun, history []message.Message) map[string]bool {
	seen := map[string]bool{}
	receipts, _ := sourceReadReceipts(run, history)
	for _, receipt := range receipts {
		seen[normalizedToolPath(receipt.Path)] = true
	}
	return seen
}
