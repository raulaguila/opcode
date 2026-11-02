package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
)

var mentionPattern = regexp.MustCompile(`(?:^|\s)@([^\s@]+)`)

// mentionedFiles lists the @path mentions in text that name a file or folder
// of the workspace, relative to its root. Paths outside the root are ignored.
func mentionedFiles(text, root string) []string {
	var found []string
	for _, match := range mentionPattern.FindAllStringSubmatch(text, -1) {
		path := filepath.Clean(strings.TrimRight(match[1], ".,;:!?)\"'"))
		if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, path)); err == nil && !slices.Contains(found, path) {
			found = append(found, filepath.ToSlash(path))
		}
	}
	return found
}

// withMentionedFiles tells the model where the user's @ mentions point: a bare
// "@main.go" means the one at the workspace root, not a main.go found by
// search elsewhere. Only the request changes; the stored message does not.
func withMentionedFiles(history []message.Message) []message.Message {
	root := config.WorkingDirectory()
	var out []message.Message
	for i, msg := range history {
		if msg.Role != message.User || msg.IsApplication() {
			continue
		}
		text := msg.Content().Text
		files := mentionedFiles(text, root)
		if len(files) == 0 {
			continue
		}
		if out == nil {
			out = slices.Clone(history)
		}
		parts := slices.Clone(msg.Parts)
		for j, part := range parts {
			if content, ok := part.(message.TextContent); ok {
				content.Text = text + "\n\n[Files mentioned with @, as paths from the workspace root: " + strings.Join(files, ", ") + "]"
				parts[j] = content
				break
			}
		}
		out[i].Parts = parts
	}
	if out == nil {
		return history
	}
	return out
}
