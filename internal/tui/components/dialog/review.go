package dialog

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diff"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

// Split only file headers; every file remains visible, including binary summaries.
func diffFiles(raw string) []string {
	lines := strings.Split(raw, "\n")
	parts := []string{}
	start := 0
	for i, line := range lines {
		if i > start && strings.HasPrefix(line, "--- a/") {
			parts = append(parts, strings.Join(lines[start:i], "\n"))
			start = i
		}
	}
	parts = append(parts, strings.Join(lines[start:], "\n"))
	return parts
}
func reviewDiff(raw string, width int, mode string) string {
	width = max(1, width)
	raw = ansi.Strip(raw)
	raw = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, raw)
	split := mode == "split" || (mode == "auto" && width >= 120)
	var out []string
	for _, part := range diffFiles(raw) {
		if split && width >= 60 {
			view, err := diff.FormatDiff(part, diff.WithTotalWidth(width))
			if err == nil && view != "" {
				for _, line := range strings.Split(part, "\n") {
					if strings.HasPrefix(line, "+++ ") {
						out = append(out, ansi.Truncate(config.Text("Arquivo: ", "File: ")+strings.TrimPrefix(line, "+++ "), width, "…"))
						break
					}
				}
				out = append(out, view)
				continue
			}
		}
		for _, line := range strings.Split(part, "\n") {
			style := styles.BaseStyle()
			if strings.HasPrefix(line, "+") {
				style = style.Foreground(theme.CurrentTheme().Success())
			}
			if strings.HasPrefix(line, "-") {
				style = style.Foreground(theme.CurrentTheme().Error())
			}
			out = append(out, style.Render(ansi.Wrap(line, width, "")))
		}
	}
	return strings.Join(out, "\n")
}
func (p *permissionDialogCmp) diffContent(raw string) string {
	key := fmt.Sprintf("%s:%d:%s", p.permission.ID, p.contentViewPort.Width, p.diffMode)
	rendered := p.GetOrSetDiff(key, func() (string, error) { return reviewDiff(raw, p.contentViewPort.Width, p.diffMode), nil })
	p.activeDiff = rendered
	p.contentViewPort.SetContent(rendered)
	return p.styleViewport()
}
func (p *permissionDialogCmp) nextFile(delta int) {
	// Rendered headers delimit files in unified and split views. Search the cached
	// full rendering rather than truncate or alter the permission payload.
	body := p.activeDiff
	lines := strings.Split(ansi.Strip(body), "\n")
	offsets := []int{0}
	for i, line := range lines {
		if i > 0 && (strings.HasPrefix(line, "--- a/") || strings.Contains(line, "File:") || strings.Contains(line, "Arquivo:")) {
			offsets = append(offsets, i)
		}
	}
	current := p.contentViewPort.YOffset
	target := 0
	if delta > 0 {
		target = max(0, len(lines)-p.contentViewPort.Height)
		for _, y := range offsets {
			if y > current {
				target = y
				break
			}
		}
	} else {
		for _, y := range offsets {
			if y < current {
				target = y
			}
		}
	}
	p.contentViewPort.SetYOffset(target)
}

// RenderReviewDiff renders application-generated unified patches for review panes.
func RenderReviewDiff(raw string, width int, mode string) string { return reviewDiff(raw, width, mode) }
