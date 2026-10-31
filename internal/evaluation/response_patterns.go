package evaluation

import "regexp"

// Patterns assert declared fixture wording; they do not judge arbitrary semantics.
func responseMatches(response string, criterion Criterion) bool {
	pattern, err := regexp.Compile(criterion.Value)
	return criterion.Value != "" && err == nil && pattern.MatchString(response) == (criterion.Kind == "response_matches")
}
