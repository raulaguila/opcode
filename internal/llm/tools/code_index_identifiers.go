package tools

import (
	"strings"
	"unicode"
)

func codeQueryIdentifiers(query string) []string {
	values := []string{strings.ToLower(strings.TrimSpace(query))}
	for _, token := range strings.Fields(query) {
		token = strings.Trim(token, "`(),;?!\"'")
		if len(token) > 160 || !codeIdentifier(token) {
			continue
		}
		lower := strings.ToLower(token)
		if !containsQueryIdentifier(values, lower) {
			values = append(values, lower)
		}
		if index := strings.LastIndexAny(lower, ".:"); index >= 0 && index+1 < len(lower) && !containsQueryIdentifier(values, lower[index+1:]) {
			values = append(values, lower[index+1:])
		}
		if len(values) >= 13 {
			break
		}
	}
	return values[:min(13, len(values))]
}

func codeIdentifier(token string) bool {
	marker := false
	for i, r := range token {
		if r == '_' || r == '.' || r == ':' {
			marker = true
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
		marker = marker || (i > 0 && unicode.IsUpper(r))
	}
	return marker
}

func containsQueryIdentifier(values []string, value string) bool {
	for _, existing := range values {
		if value == existing {
			return true
		}
	}
	return false
}

// Exact candidates precede lexical matches without widening the authorized scope.
func codeIdentifierOrder(query string) (string, []any) {
	values := codeQueryIdentifiers(query)
	matches := []string{"lower(path)=?"}
	args := []any{strings.ToLower(strings.TrimSpace(query))}
	for _, value := range values {
		matches = append(matches, "lower(symbol)=?", "substr(lower(symbol),-length(?))=?")
		args = append(args, value, "."+value, "."+value)
	}
	return "(" + strings.Join(matches, " OR ") + ") DESC, ", args
}
