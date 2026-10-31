package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
)

// Outcome describes recorded results, never an inference from tool-call counts.
func processOutcome(items []message.Message) string {
	results := map[string]message.ToolResult{}
	interrupted := false
	for _, item := range items {
		switch item.FinishReason() {
		case message.FinishReasonCanceled, message.FinishReasonMaxTokens, message.FinishReasonPermissionDenied, message.FinishReasonError:
			interrupted = true
		}
		for _, result := range item.ToolResults() {
			results[result.ToolCallID] = result
		}
	}
	failures, partial := 0, 0
	for _, result := range results {
		if result.IsError {
			failures++
		}
		flags := processResultFlags(result.Metadata)
		interrupted = interrupted || flags.Cancelled || flags.TimedOut
		if flags.Truncated {
			partial++
		}
	}
	labels := []string{config.Text("finalizado", "finished")}
	if interrupted {
		labels[0] = config.Text("interrompido", "interrupted")
	}
	if failures > 0 {
		labels = append(labels, fmt.Sprintf(config.Text("%d falha(s)", "%d failure(s)"), failures))
	}
	if partial > 0 {
		labels = append(labels, fmt.Sprintf(config.Text("%d saída(s) parcial(is)", "%d partial output(s)"), partial))
	}
	return strings.Join(labels, " · ")
}

type processFlags struct {
	Cancelled bool `json:"cancelled"`
	TimedOut  bool `json:"timed_out"`
	Truncated bool `json:"truncated"`
}

func processResultFlags(metadata string) processFlags {
	var flags processFlags
	if len(metadata) <= 4096 {
		_ = json.Unmarshal([]byte(metadata), &flags)
	}
	return flags
}
