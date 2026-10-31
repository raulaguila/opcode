package evaluation

import (
	"regexp"
	"strconv"
	"strings"
)

func citationAnchor(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil || strconv.Itoa(n) != value {
		return 0
	}
	return n
}

func fixtureSourceLines(files map[string]string, path string) int {
	source, ok := files[path]
	if !ok || source == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(source, "\n"), "\n") + 1
}

func citationAnchorExists(files map[string]string, criterion Criterion) bool {
	anchor := citationAnchor(criterion.Value)
	return anchor > 0 && anchor <= fixtureSourceLines(files, criterion.Path)
}

// This measures citation coverage of a declared fixture line, not whether the
// prose follows from it or a native read occurred. The agent owns those gates.
func responseCitesAnchor(files map[string]string, response string, criterion Criterion) bool {
	if !safePath(criterion.Path) || !citationAnchorExists(files, criterion) {
		return false
	}
	pattern := regexp.MustCompile(`(?:\./)?` + regexp.QuoteMeta(criterion.Path) + ":([0-9]+)(?:-([0-9]+))?")
	anchor, lastLine := citationAnchor(criterion.Value), fixtureSourceLines(files, criterion.Path)
	for _, match := range pattern.FindAllStringSubmatchIndex(response, -1) {
		if citationReferenceBoundary(response, match[0], match[1]) && citationReferenceCovers(response, match, anchor, lastLine) {
			return true
		}
	}
	return false
}

func citationReferenceBoundary(response string, start, end int) bool {
	const prefix = " \t\r\n([`\"'"
	const suffix = " \t\r\n)]`\"'.,;"
	return (start == 0 || strings.ContainsRune(prefix, rune(response[start-1]))) &&
		(end == len(response) || strings.ContainsRune(suffix, rune(response[end])))
}

func citationReferenceCovers(response string, match []int, anchor, lastLine int) bool {
	first, err := strconv.Atoi(response[match[2]:match[3]])
	if err != nil || first < 1 {
		return false
	}
	last := first
	if match[4] >= 0 {
		last, err = strconv.Atoi(response[match[4]:match[5]])
	}
	return err == nil && first <= anchor && anchor <= last && last <= lastLine
}
