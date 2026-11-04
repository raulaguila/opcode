package agent

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
)

type sourceReference struct {
	path        string
	first, last int
}

// Recognize path:line[-line] and GitHub-style path#Lline[-Lline], including
// Markdown links with spaces in their destination. Port numbers/timestamps and
// remote URLs are not local source references.
var citationSuffix = regexp.MustCompile(`^(.*?)(?::L?([0-9]{1,9})(?:[-–]L?([0-9]{1,9}))?|#L([0-9]{1,9})(?:-L?([0-9]{1,9}))?)$`)
var plainCitation = regexp.MustCompile(`([\p{L}\p{N}_./\\-]+)(?::L?([0-9]{1,9})(?:[-–]L?([0-9]{1,9}))?|#L([0-9]{1,9})(?:-L?([0-9]{1,9}))?)`)
var markdownCitation = regexp.MustCompile(`\]\(([^()\r\n]+)\)`)
var quotedCitation = regexp.MustCompile("`([^`\\r\\n]+)`")
var remoteCitation = regexp.MustCompile("[[:alpha:]][[:alnum:]+.-]*://[^\\s)<>`]+")

func sourceReferences(answer string) []sourceReference {
	var out []sourceReference
	seen := map[string]bool{}
	// Remove Markdown destinations before scanning plain text, so the filename
	// fragment of a path containing spaces cannot become a second false citation.
	for _, match := range markdownCitation.FindAllStringSubmatch(answer, 64) {
		addSourceReference(&out, seen, citationSuffix.FindStringSubmatch(strings.Trim(match[1], "<> ")))
	}
	answer = markdownCitation.ReplaceAllString(answer, "]()")
	answer = quotedCitation.ReplaceAllStringFunc(answer, func(value string) string {
		match := citationSuffix.FindStringSubmatch(strings.Trim(value, "`"))
		if len(match) == 0 {
			return value
		}
		addSourceReference(&out, seen, match)
		return ""
	})
	answer = remoteCitation.ReplaceAllString(answer, "")
	for _, match := range plainCitation.FindAllStringSubmatch(answer, 128) {
		addSourceReference(&out, seen, match)
	}
	return out
}

func citationDisplayPath(path string) string {
	if relative, err := filepath.Rel(config.WorkingDirectory(), path); err == nil && filepath.IsLocal(relative) {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

func addSourceReference(out *[]sourceReference, seen map[string]bool, match []string) {

	if len(match) != 6 {
		return
	}
	path := strings.Trim(match[1], "<> ")
	if strings.Contains(path, "://") || strings.Contains(path, "\x00") {
		return
	}
	base := filepath.Base(path)
	if !strings.ContainsAny(path, "/\\.") && base != "Dockerfile" && base != "Makefile" && base != "README" && base != "LICENSE" && base != "AGENTS" {
		return
	}
	start, end := match[2], match[3]
	if start == "" {
		start, end = match[4], match[5]
	}
	if end == "" {
		end = start
	}
	first, _ := strconv.Atoi(start)
	last, _ := strconv.Atoi(end)
	key := fmt.Sprintf("%s:%d-%d", path, first, last)
	if !seen[key] && len(*out) < 64 {
		*out = append(*out, sourceReference{path, first, last})
		seen[key] = true
	}

}
