package tools

import (
	"path/filepath"
	"slices"
	"strings"
)

// Prefer declared identifiers and source paths over incidental query literals.
// This is navigation ranking; results still pass the normal current-hash guard.
func overviewIndexCandidates(hits []codeIndexHit, query string) []codeIndexHit {
	terms := strings.Fields(strings.ToLower(query))
	slices.SortStableFunc(hits, func(a, b codeIndexHit) int {
		return overviewIndexScore(b, terms) - overviewIndexScore(a, terms)
	})
	if len(hits) == 0 || overviewIndexScore(hits[0], terms) <= 0 {
		return hits
	}
	end := 0
	for end < len(hits) && overviewIndexScore(hits[end], terms) > 0 {
		end++
	}
	return hits[:end]
}

func overviewIndexScore(hit codeIndexHit, terms []string) int {
	score := 0
	path := strings.ToLower(filepath.ToSlash(hit.Path))
	if projectFileRole(path) == "test" || projectFileRole(path) == "documentation" {
		return -100
	}
	for _, term := range terms {
		if strings.Contains(strings.ToLower(hit.Symbol), term) {
			score += 8
		}
		if strings.Contains(path, term) {
			score += 4
		}
	}
	return score
}
