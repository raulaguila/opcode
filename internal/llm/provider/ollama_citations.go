package provider

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A request-scoped inspected range adds no provenance when its exact path is
// already cited more precisely inside that range. Unknown selections remain
// visible, and the agent still validates every reference in the actual answer.
func coveredOllamaCitation(answer, source string, inspected []string) bool {
	if !slices.Contains(inspected, source) {
		return false
	}
	colon := strings.LastIndexByte(source, ':')
	if colon < 1 {
		return false
	}
	first, last, ok := ollamaCitationLines(source[colon+1:])
	if !ok {
		return false
	}
	pattern := `(?:^|[\s(\x60])` + regexp.QuoteMeta(source[:colon]) + `:([1-9][0-9]*(?:-[1-9][0-9]*)?)(?:$|[\s),;\x60.])`
	for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(answer, 64) {
		start, end, ok := ollamaCitationLines(match[1])
		if ok && start >= first && end <= last {
			return true
		}
	}
	return false
}

func ollamaCitationLines(value string) (int, int, bool) {
	left, right, ranged := strings.Cut(value, "-")
	first, err := strconv.Atoi(left)
	if err != nil || first < 1 {
		return 0, 0, false
	}
	last := first
	if ranged {
		last, err = strconv.Atoi(right)
	}
	return first, last, err == nil && last >= first
}
