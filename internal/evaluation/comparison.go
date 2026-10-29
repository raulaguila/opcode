package evaluation

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
)

// Comparison never mixes unavailable criteria or metrics into a success rate.
// Conditions concern the experiment, not equivalence of application policies.
type Comparison struct {
	Rankable           bool         `json:"rankable"`
	RankingUnavailable []string     `json:"ranking_unavailable"`
	Version            int          `json:"version"`
	Comparable         bool         `json:"comparable_conditions"`
	Differences        []string     `json:"differences"`
	Runs               []RunSummary `json:"runs"`
	Notice             string       `json:"notice"`
}
type RunSummary struct {
	PhaseMetrics        *agent.PhaseMetrics `json:"phase_metrics"`
	Application         string              `json:"application"`
	Version             string              `json:"application_version,omitempty"`
	Experiment          string              `json:"experiment,omitempty"`
	Passed              int                 `json:"passed_cases"`
	Failed              int                 `json:"failed_cases"`
	Unavailable         int                 `json:"unavailable_cases"`
	ObservedCriteria    int                 `json:"observed_criteria"`
	PassedCriteria      int                 `json:"passed_criteria"`
	UnavailableCriteria int                 `json:"unavailable_criteria"`
	DurationMS          int64               `json:"duration_ms"`
	InputTokens         *int64              `json:"input_tokens"`
	OutputTokens        *int64              `json:"output_tokens"`
	RetrievalRecall     *float64            `json:"retrieval_recall"`
	RetrievalMRR        *float64            `json:"retrieval_mrr"`
	PromptVersions      []string            `json:"prompt_versions"`
	PromptProfiles      []string            `json:"prompt_profiles"`
	APIModels           []string            `json:"api_models"`
	Protocols           []string            `json:"protocols"`
	ContextWindows      []int64             `json:"context_windows"`
	OutputReserves      []int64             `json:"output_reserves"`
	Temperatures        []float64           `json:"temperatures"`
	ToolCalls           *int                `json:"tool_calls"`
}

func Compare(paths []string) (Comparison, error) {
	result := Comparison{Version: 1, Comparable: true, Rankable: true, Differences: []string{}, RankingUnavailable: []string{}, Notice: "Same fixture hash, case/trial/criteria, requested model, protocol, effective context and output reserve are required. Applications retain different prompts/tools/policies. Missing metrics are null; unavailable criteria are not passed. Rankable concerns only complete declared observations, not general superiority or causal prompt improvements."}
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
		if err := validateComparisonCases(report); err != nil {
			return result, fmt.Errorf("comparison report %d: %w", index+1, err)
		}
		result.Differences = append(result.Differences, requestedDifferences(report)...)
		if index == 0 {
			baseline = report
		} else {
			if report.SuiteSHA256 != baseline.SuiteSHA256 {
				result.Differences = append(result.Differences, "fixture suite hash differs")
			}
			result.Differences = append(result.Differences, conditionDifferences(baseline, report)...)
		}
		summary := RunSummary{Experiment: tools.RedactCredentials(report.Experiment), PromptVersions: []string{}, PromptProfiles: []string{}}
		input, output, recall, mrr := int64(0), int64(0), 0., 0.
		allInput, allOutput, allRetrieval := true, true, true
		toolCalls, allToolCalls := 0, true
		phases, allPhases := agent.PhaseMetrics{}, true
		for _, c := range report.Cases {
			label := caseLabel(c)
			for _, field := range c.ConditionsUnavailable {
				result.RankingUnavailable = append(result.RankingUnavailable, label+": effective "+field+" unavailable; requested value is not an observation")
			}
			if c.Temperature == nil {
				result.RankingUnavailable = append(result.RankingUnavailable, label+": sampling temperature unavailable")
			}
			if c.CriteriaSHA256 == "" {
				result.RankingUnavailable = append(result.RankingUnavailable, label+": full criterion identity unavailable in legacy report")
			}
			if c.Status == "unavailable" {
				result.RankingUnavailable = append(result.RankingUnavailable, label+": application/case unavailable")
			}
			if c.PhaseMetrics == nil {
				allPhases = false
			} else {
				if !addPhaseMetrics(&phases, *c.PhaseMetrics) {
					return result, fmt.Errorf("comparison report %d contains invalid phase metrics", index+1)
				}
			}
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
				if toolCalls > math.MaxInt-c.ToolCalls {
					return result, fmt.Errorf("comparison tool metrics overflow")
				}
				toolCalls += c.ToolCalls
			} else if c.ReportedToolCalls != nil {
				if toolCalls > math.MaxInt-*c.ReportedToolCalls {
					return result, fmt.Errorf("comparison tool metrics overflow")
				}
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
					result.RankingUnavailable = append(result.RankingUnavailable, comparisonDiagnostic("%s: criterion %s unavailable", label, criterion.Kind))
					summary.UnavailableCriteria++
				} else {
					summary.ObservedCriteria++
					if *criterion.Passed {
						summary.PassedCriteria++
					}
				}
			}
			if summary.DurationMS > math.MaxInt64-c.DurationMS {
				return result, fmt.Errorf("comparison duration metrics overflow")
			}
			summary.DurationMS += c.DurationMS
			if c.InputTokens == nil {
				allInput = false
			} else {
				if input > math.MaxInt64-*c.InputTokens {
					return result, fmt.Errorf("comparison input metrics overflow")
				}
				input += *c.InputTokens
			}
			if c.OutputTokens == nil {
				allOutput = false
			} else {
				if output > math.MaxInt64-*c.OutputTokens {
					return result, fmt.Errorf("comparison output metrics overflow")
				}
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
		if allPhases {
			summary.PhaseMetrics = &phases
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
	sort.Strings(result.RankingUnavailable)
	result.RankingUnavailable = slices.Compact(result.RankingUnavailable)
	result.Rankable = result.Comparable && len(result.RankingUnavailable) == 0
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

func addPhaseMetrics(total *agent.PhaseMetrics, sample agent.PhaseMetrics) bool {
	targets := []*int64{&total.ContextMS, &total.ProviderMS, &total.ToolsMS, &total.FirstTokenMS, &total.FirstTokenSamples, &total.ProviderCalls, &total.ToolCalls, &total.Repairs}
	values := []int64{sample.ContextMS, sample.ProviderMS, sample.ToolsMS, sample.FirstTokenMS, sample.FirstTokenSamples, sample.ProviderCalls, sample.ToolCalls, sample.Repairs}
	for i, value := range values {
		if value < 0 || *targets[i] > math.MaxInt64-value {
			return false
		}
	}
	for i, value := range values {
		*targets[i] += value
	}
	return true
}
