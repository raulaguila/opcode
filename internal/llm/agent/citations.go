package agent

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
)

var sourceExplanation = regexp.MustCompile(`(?i)\b(?:explique|explain|localize|locate|analise|analyze|analyse|onde|where|causa|cause)\b`)

func citationOptOut(request string) bool {
	lower := strings.ToLower(request)
	for _, restriction := range []string{"não cite", "nao cite", "do not cite", "don't cite", "sem referências", "sem referencias", "without references"} {
		if strings.Contains(lower, restriction) {
			return true
		}
	}
	return false
}

func requiresSourceCitation(request string) bool {
	if citationOptOut(request) {
		return false
	}
	for _, match := range sourceExplanation.FindAllStringIndex(request, 16) {
		words := strings.Fields(strings.ToLower(request[:match[0]]))
		if len(words) > 0 {
			switch strings.Trim(words[len(words)-1], "`\"'(),: ") {
			case "não", "nao", "nunca", "never", "not", "don't", "dont", "without", "sem":
				continue
			}
		}
		return true
	}
	return false
}

// This is a citation-presence check, not a semantic correctness score. It uses
// successful native source-read receipts only, never paths mentioned in prose,
// search results, historical summaries or failed reads.
func missingSourceCitation(run *generationRun, history []message.Message, answer string) string {
	if !run.requiresCitation {
		return ""
	}
	// Required-call bookkeeping can span a resumed request. Source citations
	// must use this turn's reads instead of reviving possibly stale old reads.
	started := run.turnMessageID == ""
	calls := map[string]string{}
	paths := []string{}
	seen := map[string]bool{}
	for _, msg := range history {
		if msg.ID == run.turnMessageID {
			started = true
		}
		if !started {
			continue
		}
		for _, call := range msg.ToolCalls() {
			switch call.Name {
			case "view", "read_lines", "read_symbol":
				calls[call.ID] = normalizedToolPath(toolFilePath(call.Input))
			}
		}
		for _, result := range msg.ToolResults() {
			path := calls[result.ToolCallID]
			if result.IsError || path == "" {
				continue
			}
			relative, err := filepath.Rel(config.WorkingDirectory(), path)
			if err == nil && filepath.IsLocal(relative) {
				path = relative
			}
			path = filepath.ToSlash(path)
			if strings.Contains(answer, path) {
				return ""
			}
			if len(paths) < 8 && len(path) <= 512 && !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
		}
	}
	if len(paths) == 0 {
		return ""
	}
	encoded, _ := json.Marshal(paths)
	return "The explanation omits its inspected source. Cite at least one actual file path and relevant line in your answer, using these successful read receipts: " + string(encoded) + ". Explain the finding in the user's language. Do not invent citations, modify files or repeat successful reads just to add a reference."
}
