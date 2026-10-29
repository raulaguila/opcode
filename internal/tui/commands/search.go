package commands

import (
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode"
)

// Both entry points match the same canonical ID and application-owned labels.
func Match(query, id, description string) bool {
	normalize := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.Is(unicode.Mn, r) {
				return -1
			}
			return unicode.ToLower(r)
		}, norm.NFD.String(s))
	}
	query = normalize(strings.TrimPrefix(strings.TrimSpace(query), "/"))
	canonical := strings.Trim(strings.TrimSpace(id), "/")
	if len([]rune(query)) == 1 {
		return strings.HasPrefix(normalize(canonical), query)
	}
	text := id + " " + description
	if pair, ok := english[canonical]; ok {
		text += " " + pair[0] + " " + pair[1]
	}
	for _, c := range Builtins {
		if strings.TrimSpace(c.ID) == strings.Trim(strings.TrimSpace(id), "/") {
			text += " " + c.Title + " " + c.Description
			title, desc := Presentation(c.ID, c.Title, c.Description)
			text += " " + title + " " + desc
			break
		}
	}
	return strings.Contains(normalize(text), query)
}

// Prefer command prefixes while retaining the contextual order for equal matches.
func Rank(query, id string) int {
	query = strings.ToLower(strings.Trim(strings.TrimSpace(query), "/"))
	id = strings.ToLower(strings.Trim(strings.TrimSpace(id), "/"))
	if id == query {
		return 0
	}
	if strings.HasPrefix(id, query) {
		return 1
	}
	return 2
}
