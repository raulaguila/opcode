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
	receipts, attempted := citationReadReceipts(run, history)
	refs := sourceReferences(answer)
	if !attempted && len(refs) == 0 && !run.requiresProjectEvidence {
		return ""
	}
	ctx := run.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	v := citationValidation{run: run, ctx: ctx, receipts: receipts, refs: refs, current: map[string]bool{}, invalid: []string{}, primaryValid: !run.requiresProjectEvidence || len(run.primaryEvidence) == 0}
	v.validate()
	if v.validCount > 0 && len(v.invalid) == 0 && v.primaryValid {
		return ""
	}
	return v.problem()
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
	// Remove Markdown destinations before scanning plain text, so the filename
	// fragment of a path containing spaces cannot become a second false citation.
	for _, match := range markdownCitation.FindAllStringSubmatch(answer, 64) {
		addSourceReference(&out, seen, citationSuffix.FindStringSubmatch(strings.Trim(match[1], "<> ")))
	}
	answer = markdownCitation.ReplaceAllString(answer, "]()")
	answer = quotedCitation.ReplaceAllStringFunc(answer, func(value string) string {
		match := citationSuffix.FindStringSubmatch(strings.Trim(value, "`"))
		if len(match) == 0 {
			return value
		}
		addSourceReference(&out, seen, match)
		return ""
	})
	answer = remoteCitation.ReplaceAllString(answer, "")
	for _, match := range plainCitation.FindAllStringSubmatch(answer, 128) {
		addSourceReference(&out, seen, match)
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
	receipts, _ := citationReadReceipts(run, history)
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
	start := sourceReadTurnStart(run, history)
	if start == len(history) {
		return nil, false
	}
	return collectSourceReadReceipts(history[start:], 0, history[start].SessionID)
}

// Historical source evidence must be an original native call/result pair, not
// a continuity summary. Retain the current-turn meaning of attempted: an old
// read must not make an unrelated answer newly require a source reference.
// Look back at most 256 messages and retain at most 32 receipts, including new
// ones. Revalidation still happens at each use, with the existing policy/time
// limits; these receipts grant neither access nor a persistent freshness cache.
func citationReadReceipts(run *generationRun, history []message.Message) ([]tools.SourceReadReceipt, bool) {
	start := sourceReadTurnStart(run, history)
	if start == len(history) {
		return nil, false
	}
	first := max(0, start-256)
	return collectSourceReadReceipts(history[first:], start-first, history[start].SessionID)
}

func sourceReadTurnStart(run *generationRun, history []message.Message) int {
	if run.turnMessageID == "" {
		return 0
	}
	for i, msg := range history {
		if msg.ID == run.turnMessageID && msg.Role == message.User && !msg.IsApplication() {
			return i
		}
	}
	return len(history)
}

func collectSourceReadReceipts(history []message.Message, currentStart int, sessionID string) ([]tools.SourceReadReceipt, bool) {
	c := sourceReadCollector{sessionID: sessionID, currentStart: currentStart, calls: map[string]message.ToolCall{}}
	for i, msg := range history {
		c.message(i, msg)
	}
	return mergeSourceReadPages(c.receipts), c.attempted
}

type citationValidation struct {
	run          *generationRun
	ctx          context.Context
	receipts     []tools.SourceReadReceipt
	refs         []sourceReference
	current      map[string]bool
	invalid      []string
	validCount   int
	primaryValid bool
}

func (v *citationValidation) isCurrent(r tools.SourceReadReceipt) bool {

	key := r.Path + "\x00" + r.Hash
	valid, exists := v.current[key]
	if !exists {
		valid = tools.SourceReadCurrent(v.ctx, r)
		v.current[key] = valid
	}
	return valid

}
func (v *citationValidation) validate() {
	for _, ref := range v.refs {
		matched := false
		for _, receipt := range v.receipts {
			if normalizedToolPath(ref.path) == normalizedToolPath(receipt.Path) && ref.first >= receipt.StartLine && ref.last >= ref.first && ref.last <= receipt.EndLine && v.isCurrent(receipt) {
				matched = true
				break
			}
		}
		if matched {
			v.validCount++
			v.primaryValid = v.primaryValid || v.run.primaryEvidence[normalizedToolPath(ref.path)]
		} else if len(v.invalid) < 8 {
			v.invalid = append(v.invalid, ref.path+":"+strconv.Itoa(ref.first)+"-"+strconv.Itoa(ref.last))
		}
	}

}
func (v *citationValidation) available() []string {
	available := []string{}
	seen := map[string]bool{}
	for i := len(v.receipts) - 1; i >= 0 && len(available) < 8; i-- {
		r := v.receipts[i]
		if !v.isCurrent(r) || (v.run.requiresProjectEvidence && len(v.run.primaryEvidence) > 0 && !v.run.primaryEvidence[normalizedToolPath(r.Path)]) {
			continue
		}
		path := citationDisplayPath(r.Path)
		entry := fmt.Sprintf("%s:%d-%d", path, r.StartLine, r.EndLine)
		if !seen[entry] {
			available = append(available, entry)
			seen[entry] = true
		}
	}

	return available
}
func (v *citationValidation) problem() string {
	available := v.available()
	encoded, _ := json.Marshal(available)
	problem := "The explanation has missing or invalid source citations. "
	if len(v.refs) == 0 {
		problem += "No file:line reference was supplied. "
	}
	if !v.primaryValid {
		problem += "For this project question, include the relevant topic source supplied by context preparation; a manifest/entry point alone does not establish the requested features. "
	}
	if len(v.invalid) > 0 {
		bad, _ := json.Marshal(v.invalid)
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

type sourceReadCollector struct {
	sessionID    string
	currentStart int
	calls        map[string]message.ToolCall
	receipts     []tools.SourceReadReceipt
	attempted    bool
}

func (c *sourceReadCollector) message(index int, msg message.Message) {
	if msg.SessionID != c.sessionID {
		return
	}
	switch msg.Role {
	case message.User:
		clear(c.calls)
	case message.Assistant:
		c.captureCalls(msg)
	case message.Tool:
		for _, result := range msg.ToolResults() {
			c.result(index, result)
		}
	}
}
func (c *sourceReadCollector) captureCalls(msg message.Message) {
	for _, call := range msg.ToolCalls() {
		switch call.Name {
		case "view", "read_lines", "read_symbol":
			if call.ID != "" {
				c.calls[call.ID] = call
			}
		}
	}
}
func (c *sourceReadCollector) result(index int, result message.ToolResult) {
	call, exists := c.calls[result.ToolCallID]
	delete(c.calls, result.ToolCallID)
	if result.IsError || !exists {
		return
	}
	c.attempted = c.attempted || index >= c.currentStart
	receipt, ok := tools.VisibleSourceReadReceipt(call.Name, result.Content, result.Metadata)
	if !ok || normalizedToolPath(receipt.Path) != normalizedToolPath(toolFilePath(call.Input)) {
		return
	}
	c.receipts = append(c.receipts, receipt)
	if len(c.receipts) > 32 {
		c.receipts = c.receipts[len(c.receipts)-32:]
	}
}

func mergeSourceReadPages(receipts []tools.SourceReadReceipt) []tools.SourceReadReceipt {
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
	return merged
}

func addSourceReference(out *[]sourceReference, seen map[string]bool, match []string) {

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
	if !seen[key] && len(*out) < 64 {
		*out = append(*out, sourceReference{path, first, last})
		seen[key] = true
	}

}
