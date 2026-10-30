package styles

import (
	"reflect"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/yuin/goldmark/ast"
)

const markdownCacheBytes = 1 << 20
const markdownCacheEntries = 128

type markdownIdentity struct {
	width, prose, profile int
	theme                 uintptr
	dark                  bool
	name                  string
}

// MarkdownCache belongs to one live response. It retains bounded rendered blocks,
// never an AST, and re-parses each snapshot so later references can change it.
type MarkdownCache struct {
	identity markdownIdentity
	entries  map[string]string
	bytes    int
}

func (c *MarkdownCache) Reset() { *c = MarkdownCache{} }
func (c *MarkdownCache) Render(content string, width, prose int) string {
	identity := markdownIdentity{width: width, prose: prose, profile: int(lipgloss.ColorProfile()), dark: lipgloss.HasDarkBackground(), name: theme.CurrentThemeName()}
	value := reflect.ValueOf(theme.CurrentTheme())
	if value.Kind() == reflect.Pointer {
		identity.theme = value.Pointer()
	}
	if c.identity != identity {
		c.Reset()
		c.identity = identity
	}
	return renderMarkdownBlocks(content, width, prose, c)
}
func stableMarkdownKey(node ast.Node, content string) (string, bool) {
	if node.NextSibling() == nil || node.Kind() != ast.KindParagraph || node.Lines().Len() == 0 {
		return "", false
	}
	safe := true
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && (n.Kind() == ast.KindLink || n.Kind() == ast.KindImage || n.Kind() == ast.KindAutoLink || n.Kind() == ast.KindRawHTML) {
			safe = false
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	if !safe {
		return "", false
	}
	start, end := node.Lines().At(0).Start, node.Lines().At(node.Lines().Len()-1).Stop
	if end-start > 8192 {
		return "", false
	}
	return content[start:end], true
}

func (c *MarkdownCache) store(key string, rendered string) {
	size := len(key) + len(rendered) + 64
	if size > markdownCacheBytes || len(c.entries) >= markdownCacheEntries || c.bytes+size > markdownCacheBytes {
		return
	}
	if c.entries == nil {
		c.entries = map[string]string{}
	}
	key = strings.Clone(key)
	c.entries[key] = rendered
	c.bytes += size
}
