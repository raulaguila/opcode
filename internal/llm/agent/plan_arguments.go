package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/workflow"
)

func simplePlanArguments(run *generationRun) bool {
	return run.provider != nil && run.provider.Model().Provider == models.ProviderLocal && run.provider.Model().API == "ollama"
}

// Native local tool templates may stringify nested arrays. Use an explicitly
// advertised flat wire schema rather than evaluating Python literals or silently
// coercing arbitrary tool arguments. Store validation and permission semantics
// are identical to the structured schema used by other connections.
func planArguments(run *generationRun, raw string) (string, []workflow.Step, []string, []string, error) {
	if !simplePlanArguments(run) {
		var input struct {
			Objective        string
			Steps            []workflow.Step
			Criteria, Checks []string
		}
		if json.Unmarshal([]byte(raw), &input) != nil {
			return "", nil, nil, nil, fmt.Errorf("invalid plan: steps must be an array of {title,status}; criteria/checks must be arrays of strings")
		}
		return input.Objective, input.Steps, input.Criteria, input.Checks, nil
	}
	var input struct {
		Objective string  `json:"objective"`
		Steps     *string `json:"steps_text"`
		Criteria  *string `json:"criteria_text"`
		Checks    *string `json:"checks_text"`
	}
	if json.Unmarshal([]byte(raw), &input) != nil || input.Steps == nil || input.Criteria == nil || input.Checks == nil {
		return "", nil, nil, nil, fmt.Errorf("supply objective, steps_text (one status|title per line), criteria_text (one criterion per line), checks_text (configured names per line, empty if none); all four fields are strings")
	}
	lines := func(value string) []string {
		var out []string
		for line := range strings.SplitSeq(value, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				out = append(out, line)
			}
		}
		return out
	}
	var steps []workflow.Step
	for _, line := range lines(*input.Steps) {
		status, title, ok := strings.Cut(line, "|")
		if !ok {
			return "", nil, nil, nil, fmt.Errorf("steps_text requires one status|title per line; status is pending, in_progress or completed")
		}
		steps = append(steps, workflow.Step{Title: strings.TrimSpace(title), Status: strings.TrimSpace(status)})
	}
	return input.Objective, steps, lines(*input.Criteria), lines(*input.Checks), nil
}
