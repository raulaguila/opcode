package evaluation

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/raulaguila/opcode/internal/llm/tools"
)

func caseKey(c CaseResult) string { return fmt.Sprintf("%s/%d", c.ID, max(1, c.Trial)) }
func caseLabel(c CaseResult) string {
	return comparisonDiagnostic("%s case %s trial %d", c.Application, c.ID, max(1, c.Trial))
}

// Equality always uses raw observation fields. Sanitize only display arguments,
// before %q adds another escaping layer around credential-bearing JSON paths.
func comparisonDiagnostic(format string, args ...any) string {
	for i, value := range args {
		if text, ok := value.(string); ok {
			text = tools.RedactCredentials(text)
			runes := []rune(text)
			if len(runes) > 512 {
				text = string(runes[:512]) + "…"
			}
			args[i] = text
		}
	}
	return fmt.Sprintf(format, args...)
}

func validateComparisonCases(r Report) error {
	seen := map[string]bool{}
	for _, c := range r.Cases {
		if c.ID == "" || len(c.ID) > 80 || c.Trial < 0 || c.Trial > 3 || len(c.Criteria) == 0 || len(c.Criteria) > 16 || c.DurationMS < 0 {
			return fmt.Errorf("invalid case/trial/criteria")
		}
		if seen[caseKey(c)] {
			return fmt.Errorf("%s", comparisonDiagnostic("duplicate case/trial %s", caseKey(c)))
		}
		seen[caseKey(c)] = true
		if err := validateCaseMeasurements(c); err != nil {
			return err
		}
	}
	if r.Repeats > 3 || r.Repeats < 0 {
		return fmt.Errorf("invalid repetition count")
	}
	return validateSelectedTrials(r, seen)
}

func observedFields(c CaseResult) map[string]string {
	raw, _ := json.Marshal(c.Temperature)
	criteria := []string{}
	for _, v := range c.Criteria {
		criteria = append(criteria, v.Kind+":"+v.Path)
	}
	identity, _ := json.Marshal(criteria)
	return map[string]string{"api_model": c.APIModel, "protocol": c.Protocol, "context_window": fmt.Sprint(c.ContextWindow), "output_reserve": fmt.Sprint(c.OutputReserve), "temperature": string(raw), "criteria": string(identity), "criteria_sha256": c.CriteriaSHA256}
}

func conditionDifferences(base, candidate Report) []string {
	result := []string{}
	if base.ManifestSHA256 != candidate.ManifestSHA256 {
		result = append(result, "shared experiment manifest hash differs")
	}
	previous := map[string]CaseResult{}
	for _, c := range base.Cases {
		previous[caseKey(c)] = c
	}
	for _, c := range candidate.Cases {
		original, ok := previous[caseKey(c)]
		if !ok {
			result = append(result, caseLabel(c)+": unexpected case/trial")
			continue
		}
		delete(previous, caseKey(c))
		want, got := observedFields(original), observedFields(c)
		for _, field := range []string{"api_model", "protocol", "context_window", "output_reserve", "temperature", "criteria", "criteria_sha256"} {
			if want[field] != got[field] {
				result = append(result, comparisonDiagnostic("%s: %s differs (baseline %q, observed %q)", caseLabel(c), field, want[field], got[field]))
			}
		}
	}
	for _, c := range previous {
		result = append(result, comparisonDiagnostic("%s: missing case %s trial %d", candidate.Cases[0].Application, c.ID, max(1, c.Trial)))
	}
	return result
}

func requestedDifferences(r Report) []string {
	if r.RequestedConditions == nil {
		return nil
	}
	want := r.RequestedConditions
	temperature, _ := json.Marshal(want.Temperature)
	expected := map[string]string{"api_model": want.APIModel, "protocol": want.Protocol, "context_window": fmt.Sprint(want.ContextWindow), "output_reserve": fmt.Sprint(want.OutputReserve), "temperature": string(temperature)}
	result := []string{}
	for _, c := range r.Cases {
		observed := observedFields(c)
		for _, field := range []string{"api_model", "protocol", "context_window", "output_reserve", "temperature"} {
			if expected[field] != observed[field] {
				result = append(result, comparisonDiagnostic("%s: %s differs from manifest (requested %q, observed %q)", caseLabel(c), field, expected[field], observed[field]))
			}
		}
	}
	slices.Sort(result)
	return result
}

func validateCaseMeasurements(c CaseResult) error {
	if c.CriteriaSHA256 != "" {
		raw, err := hex.DecodeString(c.CriteriaSHA256)
		if err != nil || len(raw) != 32 {
			return fmt.Errorf("invalid criterion digest")
		}
	}
	if c.Temperature != nil && (math.IsNaN(*c.Temperature) || math.IsInf(*c.Temperature, 0) || *c.Temperature < 0 || *c.Temperature > 2) {
		return fmt.Errorf("invalid sampling temperature")
	}
	if c.InputTokens != nil && *c.InputTokens < 0 || c.OutputTokens != nil && *c.OutputTokens < 0 || c.ToolCalls < 0 || c.ReportedToolCalls != nil && *c.ReportedToolCalls < 0 {
		return fmt.Errorf("invalid negative case metrics")
	}
	return validateRetrievalConditions(c)
}

func validateSelectedTrials(r Report, seen map[string]bool) error {
	if len(r.SelectedCases) > 0 {
		ids := map[string]bool{}
		for _, id := range r.SelectedCases {
			if ids[id] {
				return fmt.Errorf("duplicate selected case")
			}
			ids[id] = true
			for trial := 1; trial <= max(1, r.Repeats); trial++ {
				if !seen[fmt.Sprintf("%s/%d", id, trial)] {
					return fmt.Errorf("%s", comparisonDiagnostic("missing case/trial %s/%d", id, trial))
				}
			}
		}
		if len(seen) != len(ids)*max(1, r.Repeats) {
			return fmt.Errorf("unexpected case/trial outside selected experiment")
		}
	}
	return nil
}

func validateRetrievalConditions(c CaseResult) error {
	for _, value := range []*float64{c.RetrievalRecall, c.RetrievalMRR} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 1) {
			return fmt.Errorf("invalid retrieval metrics")
		}
	}
	if len(c.ConditionsUnavailable) > 2 {
		return fmt.Errorf("invalid unobserved condition fields")
	}
	for _, field := range c.ConditionsUnavailable {
		if field != "context_window" && field != "output_reserve" {
			return fmt.Errorf("invalid unobserved condition field")
		}
	}
	return nil
}
