package evaluation

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/tools"
)

// Comparison never mixes unavailable criteria or metrics into a success rate.
// Conditions concern the experiment, not equivalence of application policies.
type Comparison struct {
	Version     int          `json:"version"`
	Comparable  bool         `json:"comparable_conditions"`
	Differences []string     `json:"differences"`
	Runs        []RunSummary `json:"runs"`
	Notice      string       `json:"notice"`
}
type RunSummary struct {
	Application         string    `json:"application"`
	Version             string    `json:"application_version,omitempty"`
	Experiment          string    `json:"experiment,omitempty"`
	Passed              int       `json:"passed_cases"`
	Failed              int       `json:"failed_cases"`
	Unavailable         int       `json:"unavailable_cases"`
	ObservedCriteria    int       `json:"observed_criteria"`
	PassedCriteria      int       `json:"passed_criteria"`
	UnavailableCriteria int       `json:"unavailable_criteria"`
	DurationMS          int64     `json:"duration_ms"`
	InputTokens         *int64    `json:"input_tokens"`
	OutputTokens        *int64    `json:"output_tokens"`
	RetrievalRecall     *float64  `json:"retrieval_recall"`
	RetrievalMRR        *float64  `json:"retrieval_mrr"`
	PromptVersions      []string  `json:"prompt_versions"`
	PromptProfiles      []string  `json:"prompt_profiles"`
	APIModels           []string  `json:"api_models"`
	Protocols           []string  `json:"protocols"`
	ContextWindows      []int64   `json:"context_windows"`
	OutputReserves      []int64   `json:"output_reserves"`
	Temperatures        []float64 `json:"temperatures"`
	ToolCalls           *int      `json:"tool_calls"`
}

func Compare(paths []string) (Comparison, error) {
	result := Comparison{Version: 1, Comparable: true, Differences: []string{}, Notice: "Same fixture hash, case/trial/criteria, requested model, protocol, effective context and output reserve are required. Applications retain different prompts/tools/policies. Missing metrics are null; unavailable criteria are not passed. This does not establish general superiority or causal prompt improvements."}
	if len(paths) < 2 || len(paths) > 12 {
		return result, fmt.Errorf("compare requires 2–12 live JSON reports")
	}
	var baseline Report
	for index, path := range paths {
		raw, err := readBounded(path, 2<<20)
		if err != nil {
			return result, fmt.Errorf("cannot read comparison report %d", index+1)
		}
		var report Report
		if json.Unmarshal(raw, &report) != nil || report.Version != 1 || report.Mode != "live_provider" || len(report.Cases) == 0 || len(report.Cases) > 96 || len(report.SuiteSHA256) != 64 {
			return result, fmt.Errorf("comparison report %d must be a bounded live report", index+1)
		}
		if index == 0 {
			baseline = report
		} else {
			if report.SuiteSHA256 != baseline.SuiteSHA256 {
				result.Differences = append(result.Differences, "fixture suite hash differs")
			}
			if fingerprintConditions(report) != fingerprintConditions(baseline) {
				result.Differences = append(result.Differences, "case/trial/criteria, model, protocol, effective context, output reserve or sampling temperature differs")
			}
		}
		summary := RunSummary{Experiment: tools.RedactCredentials(report.Experiment), PromptVersions: []string{}, PromptProfiles: []string{}}
		input, output, recall, mrr := int64(0), int64(0), 0., 0.
		allInput, allOutput, allRetrieval := true, true, true
		toolCalls, allToolCalls := 0, true
		for _, c := range report.Cases {
			if c.Temperature != nil && !slices.Contains(summary.Temperatures, *c.Temperature) {
				summary.Temperatures = append(summary.Temperatures, *c.Temperature)
			}
			if summary.Application == "" {
				summary.Application = tools.RedactCredentials(c.Application)
				summary.Version = tools.RedactCredentials(c.ApplicationVersion)
			}
			if c.Application == "" || c.Application != summary.Application {
				return result, fmt.Errorf("comparison report %d mixes or omits application names", index+1)
			}
			if c.APIModel == "" || c.Protocol == "" || c.ContextWindow <= 0 || c.OutputReserve <= 0 {
				result.Differences = append(result.Differences, "effective model/protocol/context/output information missing")
			}
			if !contains(summary.APIModels, c.APIModel) {
				summary.APIModels = append(summary.APIModels, tools.RedactCredentials(c.APIModel))
			}
			if !contains(summary.Protocols, c.Protocol) {
				summary.Protocols = append(summary.Protocols, tools.RedactCredentials(c.Protocol))
			}
			if !containsInt64(summary.ContextWindows, c.ContextWindow) {
				summary.ContextWindows = append(summary.ContextWindows, c.ContextWindow)
			}
			if !containsInt64(summary.OutputReserves, c.OutputReserve) {
				summary.OutputReserves = append(summary.OutputReserves, c.OutputReserve)
			}
			if c.Application == "opcode" {
				toolCalls += c.ToolCalls
			} else if c.ReportedToolCalls != nil {
				toolCalls += *c.ReportedToolCalls
			} else {
				allToolCalls = false
			}
			switch c.Status {
			case "passed":
				summary.Passed++
			case "failed":
				summary.Failed++
			case "unavailable":
				summary.Unavailable++
			default:
				return result, fmt.Errorf("comparison report contains non-live status")
			}
			for _, criterion := range c.Criteria {
				if criterion.Passed == nil {
					summary.UnavailableCriteria++
				} else {
					summary.ObservedCriteria++
					if *criterion.Passed {
						summary.PassedCriteria++
					}
				}
			}
			summary.DurationMS += c.DurationMS
			if c.InputTokens == nil {
				allInput = false
			} else {
				input += *c.InputTokens
			}
			if c.OutputTokens == nil {
				allOutput = false
			} else {
				output += *c.OutputTokens
			}
			if c.RetrievalRecall == nil || c.RetrievalMRR == nil {
				allRetrieval = false
			} else {
				recall += *c.RetrievalRecall
				mrr += *c.RetrievalMRR
			}
			if c.PromptVersion != "" && !contains(summary.PromptVersions, c.PromptVersion) {
				summary.PromptVersions = append(summary.PromptVersions, tools.RedactCredentials(c.PromptVersion))
			}
			if c.PromptProfile != "" && !contains(summary.PromptProfiles, c.PromptProfile) {
				summary.PromptProfiles = append(summary.PromptProfiles, tools.RedactCredentials(c.PromptProfile))
			}
		}
		if allInput {
			summary.InputTokens = &input
		}
		if allOutput {
			summary.OutputTokens = &output
		}
		if allToolCalls {
			summary.ToolCalls = &toolCalls
		}
		if allRetrieval {
			recall /= float64(len(report.Cases))
			mrr /= float64(len(report.Cases))
			summary.RetrievalRecall = &recall
			summary.RetrievalMRR = &mrr
		}
		result.Runs = append(result.Runs, summary)
	}
	sort.Strings(result.Differences)
	unique := result.Differences[:0]
	for _, d := range result.Differences {
		if !contains(unique, d) {
			unique = append(unique, d)
		}
	}
	result.Differences = unique
	result.Comparable = len(result.Differences) == 0
	return result, nil
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func fingerprintConditions(report Report) string {
	rows := make([]string, 0, len(report.Cases))
	for _, c := range report.Cases {
		criteria := []string{}
		for _, v := range c.Criteria {
			criteria = append(criteria, v.Kind+":"+v.Path)
		}
		temperature, _ := json.Marshal(c.Temperature)
		rows = append(rows, fmt.Sprintf("%s|%d|%s|%s|%d|%d|%s|%s", c.ID, c.Trial, c.APIModel, c.Protocol, c.ContextWindow, c.OutputReserve, string(temperature), strings.Join(criteria, ",")))
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}
