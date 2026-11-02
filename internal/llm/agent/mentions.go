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

var mentionPattern = regexp.MustCompile(`(^|\s)@([^\s@]+)`)

// resolveMentions rewrites each @path that names a file or folder of the
// workspace as ./path, so "@main.go" means the one at the root rather than a
// main.go found by search. It costs about one token per mention. Paths
// outside the root and names that do not exist are left as written.
func resolveMentions(text, root string) string {
	return mentionPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := mentionPattern.FindStringSubmatch(match)
		lead, raw := parts[1], parts[2]
		trimmed := strings.TrimRight(raw, ".,;:!?)\"'")
		path := filepath.Clean(trimmed)
		if filepath.IsAbs(path) || !filepath.IsLocal(path) {
			return match
		}
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			return match
		}
		resolved := "./" + filepath.ToSlash(path)
		if info.IsDir() {
			resolved += "/"
		}
		return lead + resolved + raw[len(trimmed):]
	})
}

// withMentionedFiles applies resolveMentions to the user's messages in a
// request; the stored messages keep the @ as typed.
func withMentionedFiles(history []message.Message) []message.Message {
	root := config.WorkingDirectory()
	var out []message.Message
	for i, msg := range history {
		if msg.Role != message.User || msg.IsApplication() || !strings.Contains(msg.Content().Text, "@") {
			continue
		}
		for j, part := range msg.Parts {
			content, ok := part.(message.TextContent)
			if !ok {
				continue
			}
			if resolved := resolveMentions(content.Text, root); resolved != content.Text {
				if out == nil {
					out = slices.Clone(history)
				}
				content.Text = resolved
				out[i].Parts = slices.Clone(msg.Parts)
				out[i].Parts[j] = content
			}
			break
		}
	}
	if out == nil {
		return history
	}
	return out
}
