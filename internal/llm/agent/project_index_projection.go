package agent

import (
	"encoding/json"
	"slices"
	"strconv"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type preparedIndexReference struct {
	Path               string `json:"path"`
	Symbol             string `json:"symbol,omitempty"`
	InspectedReference string `json:"inspected_reference,omitempty"`
}

type preparedIndexCandidate struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"`
	First  int    `json:"start_line"`
	Last   int    `json:"end_line"`
	Hash   string `json:"file_hash"`
}

type preparedSourceReads map[string][]tools.SourceReadReceipt

// Automatic navigation is summarized against actual reads; the saved transcript
// and native source bodies/receipts remain intact. Manual searches stay usable.
func projectPreparedIndexes(history []message.Message, run *generationRun) []message.Message {
	receipts, _ := sourceReadReceipts(run, history)
	read := preparedSourceReads{}
	for _, receipt := range receipts {
		path := normalizedToolPath(receipt.Path)
		read[path] = append(read[path], receipt)
	}
	automatic := map[string]bool{}
	for _, msg := range history {
		for _, call := range msg.ToolCalls() {
			if call.Origin == "context" && call.Name == "search_code_index" {
				automatic[call.ID] = true
			}
		}
	}
	result := slices.Clone(history)
	for i, msg := range history {
		result[i] = projectPreparedIndexMessage(msg, automatic, read)
	}
	return result
}

func projectPreparedIndexMessage(msg message.Message, automatic map[string]bool, read preparedSourceReads) message.Message {
	cloned := false
	for i, part := range msg.Parts {
		result, ok := part.(message.ToolResult)
		if !ok || result.IsError || result.Name != "search_code_index" || !automatic[result.ToolCallID] {
			continue
		}
		if !cloned {
			msg.Parts = slices.Clone(msg.Parts)
			cloned = true
		}
		result.Content = preparedIndexSummary(result.Content, read)
		msg.Parts[i] = result
	}
	return msg
}

func preparedIndexSummary(content string, read preparedSourceReads) string {
	var page struct {
		Results []preparedIndexCandidate `json:"results"`
	}
	references := []preparedIndexReference{}
	omitted := true
	if len(content) <= 6000 && json.Unmarshal([]byte(content), &page) == nil {
		omitted = false
		for _, hit := range page.Results {
			if receipts := read[normalizedToolPath(hit.Path)]; len(receipts) > 0 {
				references = append(references, preparedIndexReference{Path: hit.Path, Symbol: hit.Symbol, InspectedReference: inspectedIndexReference(hit, receipts)})
			}
		}
	}
	summary := struct {
		Results          []preparedIndexReference `json:"results"`
		NavigationOnly   bool                     `json:"navigation_only"`
		AutomaticSummary bool                     `json:"automatic_preparation_summary"`
		Omitted          bool                     `json:"navigation_details_omitted"`
		Notice           string                   `json:"notice"`
	}{references, true, true, omitted, "Navigation only. Source evidence is in successful native read results, not this summary. Use search_code_index for further candidates."}
	raw, _ := json.Marshal(summary)
	return string(raw)
}

// A complete symbol interval is only a navigation convenience; the original
// native read receipt remains the authority for every eventual citation.
func inspectedIndexReference(hit preparedIndexCandidate, receipts []tools.SourceReadReceipt) string {
	if hit.Kind == "" || hit.Kind == "text" || hit.Symbol == "" || hit.Symbol == "<file>" || hit.Hash == "" || hit.First < 1 || hit.Last < hit.First {
		return ""
	}
	for _, receipt := range receipts {
		if normalizedToolPath(hit.Path) == normalizedToolPath(receipt.Path) && hit.Hash == receipt.Hash && hit.First >= receipt.StartLine && hit.Last <= receipt.EndLine {
			return hit.Path + ":" + strconv.Itoa(hit.First) + "-" + strconv.Itoa(hit.Last)
		}
	}
	return ""
}
