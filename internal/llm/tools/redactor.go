package tools

import (
	"encoding/json"
	"slices"
	"strings"
)

// A redactor owns one operation's credential snapshot. Public output still
// passes through SecureTool's fresh snapshot to cover credentials added while
// the operation was in progress. No process-wide secret cache is retained.
type credentialRedactor struct{ representations []string }

func newCredentialRedactor(secrets []string) *credentialRedactor {
	r := &credentialRedactor{}
	seen := make(map[string]bool, len(secrets)*2)
	for _, secret := range secrets {
		encoded, _ := json.Marshal(secret)
		for _, value := range []string{secret, string(encoded[1 : len(encoded)-1])} {
			if value != "" && !seen[value] {
				seen[value] = true
				r.representations = append(r.representations, value)
			}
		}
	}
	// A shorter credential must not expose the suffix of a longer one.
	slices.SortFunc(r.representations, func(a, b string) int { return len(b) - len(a) })
	return r
}

func (r *credentialRedactor) Redact(value string) string {
	for _, secret := range r.representations {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}
