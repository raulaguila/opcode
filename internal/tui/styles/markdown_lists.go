package styles

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// Render individual items at their remaining width so continuations align with
// the text, rather than being reflowed with the entire list by Glamour.
func (b *markdownBlockRenderer) renderList(list *ast.List) (string, error) {
	width := markdownBlockWidth(list, b.width, b.prose)
	digits := len(strconv.Itoa(list.Start + max(0, list.ChildCount()-1)))
	var out strings.Builder
	index := 0
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		marker := markdownListMarker(list, item, index, digits)
		body, err := b.renderListItem(item, max(1, width-ansi.StringWidth(marker)))
		if err != nil {
			return "", err
		}
		if index > 0 {
			out.WriteByte('\n')
			if !list.IsTight {
				out.WriteByte('\n')
			}
		}
		out.WriteString(indentMarkdownListItem(body, marker))
		index++
	}
	return out.String(), nil
}

func markdownListMarker(list *ast.List, item ast.Node, index, digits int) string {
	if first := item.FirstChild(); first != nil {
		if check, ok := first.FirstChild().(*extast.TaskCheckBox); ok {
			if check.IsChecked {
				return "[✓] "
			}
			return "[ ] "
		}
	}
	if list.IsOrdered() {
		return fmt.Sprintf("%*d%c ", digits, list.Start+index, list.Marker)
	}
	return "• "
}

func (b *markdownBlockRenderer) renderListItem(item ast.Node, width int) (string, error) {
	childRenderer := *b
	childRenderer.width, childRenderer.prose = width, width
	var blocks []string
	var previous ast.NodeKind
	for child := item.FirstChild(); child != nil; {
		next := child.NextSibling()
		kind := child.Kind()
		view, err := childRenderer.renderFresh(child)
		if err != nil {
			return "", err
		}
		if view != "" {
			separator := "\n\n"
			if kind == ast.KindList || previous == ast.KindList {
				separator = "\n"
			}
			if len(blocks) > 0 {
				blocks = append(blocks, separator)
			}
			blocks = append(blocks, removeMarkdownListMargin(view))
			previous = kind
		}
		child = next
	}
	return strings.Join(blocks, ""), nil
}

// Per-block rendering trims visual padding; fenced source blank rows are content.
func preserveMarkdownCodePadding(view string, node ast.Node, source []byte) string {
	if node.Kind() != ast.KindCodeBlock && node.Kind() != ast.KindFencedCodeBlock {
		return view
	}
	lines := node.Lines()
	leading, trailing := 0, 0
	for leading < lines.Len() {
		line := lines.At(leading)
		if len(bytes.TrimSpace(line.Value(source))) > 0 {
			break
		}
		leading++
	}
	if leading == lines.Len() {
		if leading == 0 {
			return ""
		}
		// Keep an all-blank block present without counting its edges twice.
		return strings.Repeat(" \n", leading-1) + " "
	}
	for i := lines.Len() - 1; i >= leading; i-- {
		line := lines.At(i)
		if len(bytes.TrimSpace(line.Value(source))) > 0 {
			break
		}
		trailing++
	}
	return strings.Repeat("\n", leading) + view + strings.Repeat("\n", trailing)
}

func removeMarkdownListMargin(view string) string {
	var out strings.Builder
	first := true
	for line := range strings.SplitSeq(view, "\n") {
		if !first {
			out.WriteByte('\n')
		}
		out.WriteString(ansi.Cut(line, defaultMargin, ansi.StringWidth(line)))
		first = false
	}
	return out.String()
}

func indentMarkdownListItem(body, marker string) string {
	margin := strings.Repeat(" ", defaultMargin)
	indent := margin + strings.Repeat(" ", ansi.StringWidth(marker))
	return margin + marker + strings.ReplaceAll(body, "\n", "\n"+indent)
}
