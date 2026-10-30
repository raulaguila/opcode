package evaluation

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"

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

type comparisonRun struct {
	summary                           RunSummary
	input, output                     int64
	recall, mrr                       float64
	allInput, allOutput, allRetrieval bool
	toolCalls                         int
	allToolCalls                      bool
	phases                            agent.PhaseMetrics
	allPhases                         bool
}

func Compare(paths []string) (Comparison, error) {
	result := Comparison{Version: 1, Comparable: true, Rankable: true, Differences: []string{}, RankingUnavailable: []string{}, Notice: "Same fixture hash, case/trial/criteria, requested model, protocol, effective context and output reserve are required. Applications retain different prompts/tools/policies. Missing metrics are null; unavailable criteria are not passed. Rankable concerns only complete declared observations, not general superiority or causal prompt improvements."}
	if len(paths) < 2 || len(paths) > 12 {
		return result, fmt.Errorf("compare requires 2–12 live JSON reports")
	}
	var baseline Report
	for index, path := range paths {
		report, err := loadComparisonReport(path, index)
		if err != nil {
			return result, err
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
		run := comparisonRun{summary: RunSummary{Experiment: tools.RedactCredentials(report.Experiment), PromptVersions: []string{}, PromptProfiles: []string{}}, allInput: true, allOutput: true, allRetrieval: true, allToolCalls: true, allPhases: true}
		if err := run.observe(&result, report.Cases, index); err != nil {
			return result, err
		}
		result.Runs = append(result.Runs, run.finish(len(report.Cases)))
	}
	finalizeComparison(&result)
	return result, nil
}

func loadComparisonReport(path string, index int) (report Report, err error) {
	raw, err := readBounded(path, 2<<20)
	if err != nil {
		return report, fmt.Errorf("cannot read comparison report %d", index+1)
	}
	if json.Unmarshal(raw, &report) != nil || report.Version != 1 || report.Mode != "live_provider" || len(report.Cases) == 0 || len(report.Cases) > 96 || len(report.SuiteSHA256) != 64 {
		return report, fmt.Errorf("comparison report %d must be a bounded live report", index+1)
	}
	if err := validateComparisonCases(report); err != nil {
		return report, fmt.Errorf("comparison report %d: %w", index+1, err)
	}
	return report, nil
}
func (r *comparisonRun) observe(comparison *Comparison, cases []CaseResult, index int) error {
	for _, c := range cases {
		if err := r.observeCase(comparison, c, index); err != nil {
			return err
		}
	}
	return nil
}
func (r *comparisonRun) finish(caseCount int) RunSummary {
	if r.allPhases {
		r.summary.PhaseMetrics = &r.phases
	}
	if r.allInput {
		r.summary.InputTokens = &r.input
	}
	if r.allOutput {
		r.summary.OutputTokens = &r.output
	}
	if r.allToolCalls {
		r.summary.ToolCalls = &r.toolCalls
	}
	if r.allRetrieval {
		r.recall /= float64(caseCount)
		r.mrr /= float64(caseCount)
		r.summary.RetrievalRecall = &r.recall
		r.summary.RetrievalMRR = &r.mrr
	}
	return r.summary
}
func finalizeComparison(result *Comparison) {
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
}

func (r *comparisonRun) recordUnavailable(comparison *Comparison, c CaseResult, index int) error {
	label := caseLabel(c)
	for _, field := range c.ConditionsUnavailable {
		comparison.RankingUnavailable = append(comparison.RankingUnavailable, label+": effective "+field+" unavailable; requested value is not an observation")
	}
	if c.Temperature == nil {
		comparison.RankingUnavailable = append(comparison.RankingUnavailable, label+": sampling temperature unavailable")
	}
	if c.CriteriaSHA256 == "" {
		comparison.RankingUnavailable = append(comparison.RankingUnavailable, label+": full criterion identity unavailable in legacy report")
	}
	if c.Status == "unavailable" {
		comparison.RankingUnavailable = append(comparison.RankingUnavailable, label+": application/case unavailable")
	}

	return nil
}

func (r *comparisonRun) recordPhases(comparison *Comparison, c CaseResult, index int) error {
	if c.PhaseMetrics == nil {
		r.allPhases = false
	} else {
		if !addPhaseMetrics(&r.phases, *c.PhaseMetrics) {
			return fmt.Errorf("comparison report %d contains invalid phase metrics", index+1)
		}
	}

	return nil
}

func (r *comparisonRun) recordIdentity(comparison *Comparison, c CaseResult, index int) error {
	if c.Temperature != nil && !slices.Contains(r.summary.Temperatures, *c.Temperature) {
		r.summary.Temperatures = append(r.summary.Temperatures, *c.Temperature)
	}
	if r.summary.Application == "" {
		r.summary.Application = tools.RedactCredentials(c.Application)
		r.summary.Version = tools.RedactCredentials(c.ApplicationVersion)
	}
	if c.Application == "" || c.Application != r.summary.Application {
		return fmt.Errorf("comparison report %d mixes or omits application names", index+1)
	}
	if c.APIModel == "" || c.Protocol == "" || c.ContextWindow <= 0 || c.OutputReserve <= 0 {
		comparison.Differences = append(comparison.Differences, "effective model/protocol/context/output information missing")
	}
	if !contains(r.summary.APIModels, c.APIModel) {
		r.summary.APIModels = append(r.summary.APIModels, tools.RedactCredentials(c.APIModel))
	}
	if !contains(r.summary.Protocols, c.Protocol) {
		r.summary.Protocols = append(r.summary.Protocols, tools.RedactCredentials(c.Protocol))
	}
	if !containsInt64(r.summary.ContextWindows, c.ContextWindow) {
		r.summary.ContextWindows = append(r.summary.ContextWindows, c.ContextWindow)
	}
	if !containsInt64(r.summary.OutputReserves, c.OutputReserve) {
		r.summary.OutputReserves = append(r.summary.OutputReserves, c.OutputReserve)
	}

	return nil
}

func (r *comparisonRun) recordToolMetrics(comparison *Comparison, c CaseResult, index int) error {
	if c.Application == "opcode" {
		if r.toolCalls > math.MaxInt-c.ToolCalls {
			return fmt.Errorf("comparison tool metrics overflow")
		}
		r.toolCalls += c.ToolCalls
	} else if c.ReportedToolCalls != nil {
		if r.toolCalls > math.MaxInt-*c.ReportedToolCalls {
			return fmt.Errorf("comparison tool metrics overflow")
		}
		r.toolCalls += *c.ReportedToolCalls
	} else {
		r.allToolCalls = false
	}

	return nil
}

func (r *comparisonRun) recordStatus(comparison *Comparison, c CaseResult, index int) error {
	switch c.Status {
	case "passed":
		r.summary.Passed++
	case "failed":
		r.summary.Failed++
	case "unavailable":
		r.summary.Unavailable++
	default:
		return fmt.Errorf("comparison report contains non-live status")
	}

	return nil
}

func (r *comparisonRun) recordCriteria(comparison *Comparison, c CaseResult, index int) error {
	label := caseLabel(c)
	for _, criterion := range c.Criteria {
		if criterion.Passed == nil {
			comparison.RankingUnavailable = append(comparison.RankingUnavailable, comparisonDiagnostic("%s: criterion %s unavailable", label, criterion.Kind))
			r.summary.UnavailableCriteria++
		} else {
			r.summary.ObservedCriteria++
			if *criterion.Passed {
				r.summary.PassedCriteria++
			}
		}
	}

	return nil
}

func (r *comparisonRun) recordTokenMetrics(comparison *Comparison, c CaseResult, index int) error {
	if r.summary.DurationMS > math.MaxInt64-c.DurationMS {
		return fmt.Errorf("comparison duration metrics overflow")
	}
	r.summary.DurationMS += c.DurationMS
	if c.InputTokens == nil {
		r.allInput = false
	} else {
		if r.input > math.MaxInt64-*c.InputTokens {
			return fmt.Errorf("comparison input metrics overflow")
		}
		r.input += *c.InputTokens
	}
	if c.OutputTokens == nil {
		r.allOutput = false
	} else {
		if r.output > math.MaxInt64-*c.OutputTokens {
			return fmt.Errorf("comparison output metrics overflow")
		}
		r.output += *c.OutputTokens
	}

	return nil
}

func (r *comparisonRun) recordRetrieval(comparison *Comparison, c CaseResult, index int) error {
	if c.RetrievalRecall == nil || c.RetrievalMRR == nil {
		r.allRetrieval = false
	} else {
		r.recall += *c.RetrievalRecall
		r.mrr += *c.RetrievalMRR
	}

	return nil
}

func (r *comparisonRun) recordPromptIdentity(comparison *Comparison, c CaseResult, index int) error {
	if c.PromptVersion != "" && !contains(r.summary.PromptVersions, c.PromptVersion) {
		r.summary.PromptVersions = append(r.summary.PromptVersions, tools.RedactCredentials(c.PromptVersion))
	}
	if c.PromptProfile != "" && !contains(r.summary.PromptProfiles, c.PromptProfile) {
		r.summary.PromptProfiles = append(r.summary.PromptProfiles, tools.RedactCredentials(c.PromptProfile))
	}
	return nil
}

func containsInt64(values []int64, target int64) bool {
	return slices.Contains(values, target)
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

func (r *comparisonRun) observeCase(comparison *Comparison, c CaseResult, index int) error {
	if err := r.recordUnavailable(comparison, c, index); err != nil {
		return err
	}
	if err := r.recordPhases(comparison, c, index); err != nil {
		return err
	}
	if err := r.recordIdentity(comparison, c, index); err != nil {
		return err
	}
	if err := r.recordToolMetrics(comparison, c, index); err != nil {
		return err
	}
	if err := r.recordStatus(comparison, c, index); err != nil {
		return err
	}
	if err := r.recordCriteria(comparison, c, index); err != nil {
		return err
	}
	if err := r.recordTokenMetrics(comparison, c, index); err != nil {
		return err
	}
	if err := r.recordRetrieval(comparison, c, index); err != nil {
		return err
	}
	if err := r.recordPromptIdentity(comparison, c, index); err != nil {
		return err
	}

	return nil
}
