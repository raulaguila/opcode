package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
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
	if tools.ProjectInquiry(request) {
		return true
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

// Validation checks provenance, current whole-file hash and delivered line
// intervals. It does not check whether the prose correctly interprets the code.
func missingSourceCitation(run *generationRun, history []message.Message, answer string) string {
	if !run.requiresCitation {
		return ""
	}
	receipts, attempted := sourceReadReceipts(run, history)
	refs := sourceReferences(answer)
	if !attempted && len(refs) == 0 && !run.requiresProjectEvidence {
		return "" // Failed reads and navigation results are not source evidence.
	}
	ctx := run.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	current := map[string]bool{}
	isCurrent := func(r tools.SourceReadReceipt) bool {
		key := r.Path + "\x00" + r.Hash
		valid, exists := current[key]
		if !exists {
			valid = tools.SourceReadCurrent(ctx, r)
			current[key] = valid
		}
		return valid
	}
	invalid := []string{}
	validCount := 0
	primaryValid := !run.requiresProjectEvidence || len(run.primaryEvidence) == 0
	for _, ref := range refs {
		matched := false
		for _, receipt := range receipts {
			if normalizedToolPath(ref.path) == normalizedToolPath(receipt.Path) && ref.first >= receipt.StartLine && ref.last >= ref.first && ref.last <= receipt.EndLine && isCurrent(receipt) {
				matched = true
				break
			}
		}
		if matched {
			validCount++
			primaryValid = primaryValid || run.primaryEvidence[normalizedToolPath(ref.path)]
		} else if len(invalid) < 8 {
			invalid = append(invalid, ref.path+":"+strconv.Itoa(ref.first)+"-"+strconv.Itoa(ref.last))
		}
	}
	if validCount > 0 && len(invalid) == 0 && primaryValid {
		return ""
	}
	available := []string{}
	seen := map[string]bool{}
	for i := len(receipts) - 1; i >= 0 && len(available) < 8; i-- {
		r := receipts[i]
		if !isCurrent(r) || (run.requiresProjectEvidence && len(run.primaryEvidence) > 0 && !run.primaryEvidence[normalizedToolPath(r.Path)]) {
			continue
		}
		path := citationDisplayPath(r.Path)
		entry := fmt.Sprintf("%s:%d-%d", path, r.StartLine, r.EndLine)
		if !seen[entry] {
			available = append(available, entry)
			seen[entry] = true
		}
	}
	encoded, _ := json.Marshal(available)
	problem := "The explanation has missing or invalid source citations. "
	if !primaryValid {
		problem += "For this project question, include the relevant topic source supplied by context preparation; a manifest/entry point alone does not establish the requested features. "
	}
	if len(invalid) > 0 {
		bad, _ := json.Marshal(invalid)
		problem += "Unverified references (unread path, unread lines or changed source): " + string(bad) + ". "
	}
	problem += "Cite actual file paths and one-based lines only within these successful, current read intervals: " + string(encoded) + ". "
	if len(available) == 0 {
		problem += "No verifiable current interval remains. Use read_lines to inspect the relevant current source before citing it. If reading fails, report the limitation without inventing a source citation. "
	} else {
		problem += "Do not repeat successful reads just to add a reference. "
	}
	return problem + "Explain the finding in the user's language. Do not invent citations, modify files, repeat side effects, or treat search/summary prose as source evidence."
}

type sourceReference struct {
	path        string
	first, last int
}

// Recognize path:line[-line] and GitHub-style path#Lline[-Lline], including
// Markdown links with spaces in their destination. Port numbers/timestamps and
// remote URLs are not local source references.
var citationSuffix = regexp.MustCompile(`^(.*?)(?::L?([0-9]{1,9})(?:[-–]L?([0-9]{1,9}))?|#L([0-9]{1,9})(?:-L?([0-9]{1,9}))?)$`)
var plainCitation = regexp.MustCompile(`([\p{L}\p{N}_./\\-]+)(?::L?([0-9]{1,9})(?:[-–]L?([0-9]{1,9}))?|#L([0-9]{1,9})(?:-L?([0-9]{1,9}))?)`)
var markdownCitation = regexp.MustCompile(`\]\(([^()\r\n]+)\)`)
var quotedCitation = regexp.MustCompile("`([^`\\r\\n]+)`")
var remoteCitation = regexp.MustCompile("[[:alpha:]][[:alnum:]+.-]*://[^\\s)<>`]+")

func sourceReferences(answer string) []sourceReference {
	var out []sourceReference
	seen := map[string]bool{}
	add := func(match []string) {
		if len(match) != 6 {
			return
		}
		path := strings.Trim(match[1], "<> ")
		if strings.Contains(path, "://") || strings.Contains(path, "\x00") {
			return
		}
		base := filepath.Base(path)
		if !strings.ContainsAny(path, "/\\.") && base != "Dockerfile" && base != "Makefile" && base != "README" && base != "LICENSE" && base != "AGENTS" {
			return
		}
		start, end := match[2], match[3]
		if start == "" {
			start, end = match[4], match[5]
		}
		if end == "" {
			end = start
		}
		first, _ := strconv.Atoi(start)
		last, _ := strconv.Atoi(end)
		key := fmt.Sprintf("%s:%d-%d", path, first, last)
		if !seen[key] && len(out) < 64 {
			out = append(out, sourceReference{path, first, last})
			seen[key] = true
		}
	}
	// Remove Markdown destinations before scanning plain text, so the filename
	// fragment of a path containing spaces cannot become a second false citation.
	for _, match := range markdownCitation.FindAllStringSubmatch(answer, 64) {
		add(citationSuffix.FindStringSubmatch(strings.Trim(match[1], "<> ")))
	}
	answer = markdownCitation.ReplaceAllString(answer, "]()")
	answer = quotedCitation.ReplaceAllStringFunc(answer, func(value string) string {
		match := citationSuffix.FindStringSubmatch(strings.Trim(value, "`"))
		if len(match) == 0 {
			return value
		}
		add(match)
		return ""
	})
	answer = remoteCitation.ReplaceAllString(answer, "")
	for _, match := range plainCitation.FindAllStringSubmatch(answer, 128) {
		add(match)
	}
	return out
}

func citationDisplayPath(path string) string {
	if relative, err := filepath.Rel(config.WorkingDirectory(), path); err == nil && filepath.IsLocal(relative) {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

// Keep the real request explicit: a small model can otherwise answer the last
// application validation notice instead of repairing its explanation.
func citationRepairRequest(run *generationRun, history []message.Message, problem string) string {
	request := ""
	for _, msg := range history {
		if msg.ID == run.turnMessageID && msg.Role == message.User {
			request = clip(msg.Content().Text, 2400)
			break
		}
	}
	encoded, _ := json.Marshal(request)
	return "Application validation: " + problem + "\nThis is a correction to your previous answer, not a new user task. Answer the original request again in its language, retaining all requested facts and explaining the finding. Use path:line or path:start-end for verified citations. Do not summarize or quote this validation notice. Original user request: " + string(encoded)
}

func currentCitationChoices(run *generationRun, history []message.Message) []string {
	ctx := run.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	receipts, _ := sourceReadReceipts(run, history)
	choices := []string{}
	total := 0
	for _, receipt := range receipts {
		if len(choices) >= 8 || ctx.Err() != nil {
			break
		}
		if !tools.SourceReadCurrent(ctx, receipt) {
			continue
		}
		if run.requiresProjectEvidence && len(run.primaryEvidence) > 0 && !run.primaryEvidence[normalizedToolPath(receipt.Path)] {
			continue
		}
		choice := fmt.Sprintf("%s:%d-%d", citationDisplayPath(receipt.Path), receipt.StartLine, receipt.EndLine)
		if total+len(choice) > 2048 {
			continue
		}
		total += len(choice)
		choices = append(choices, choice)
	}
	return choices
}

func sourceReadReceipts(run *generationRun, history []message.Message) ([]tools.SourceReadReceipt, bool) {
	started := run.turnMessageID == ""
	calls := map[string]message.ToolCall{}
	var receipts []tools.SourceReadReceipt
	attempted := false
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
				calls[call.ID] = call
			}
		}
		for _, result := range msg.ToolResults() {
			call, exists := calls[result.ToolCallID]
			if result.IsError || !exists {
				continue
			}
			attempted = true
			receipt, ok := tools.VisibleSourceReadReceipt(call.Name, result.Content, result.Metadata)
			if !ok || normalizedToolPath(receipt.Path) != normalizedToolPath(toolFilePath(call.Input)) {
				continue
			}
			receipts = append(receipts, receipt)
			if len(receipts) > 32 {
				receipts = receipts[len(receipts)-32:]
			}
		}
	}
	// Adjacent pages of the same immutable source can support a single range;
	// a gap or different hash must never be hidden by merging their endpoints.
	sort.SliceStable(receipts, func(i, j int) bool {
		if receipts[i].Path != receipts[j].Path {
			return receipts[i].Path < receipts[j].Path
		}
		if receipts[i].Hash != receipts[j].Hash {
			return receipts[i].Hash < receipts[j].Hash
		}
		return receipts[i].StartLine < receipts[j].StartLine
	})
	merged := make([]tools.SourceReadReceipt, 0, len(receipts))
	for _, r := range receipts {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.Path == r.Path && last.Hash == r.Hash && r.StartLine <= last.EndLine+1 {
				last.EndLine = max(last.EndLine, r.EndLine)
				continue
			}
		}
		merged = append(merged, r)
	}
	return merged, attempted
}
