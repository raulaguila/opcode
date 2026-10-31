package styles

import (
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

// TerminalOutput applies capabilities at the output boundary; layout and caches
// work with full-fidelity ANSI internally.
func TerminalOutput(value string) string {
	profile := theme.ColorProfile()
	if profile == colorprofile.TrueColor {
		return value
	}
	if profile <= colorprofile.NoTTY {
		return ansi.Strip(value)
	}
	var out strings.Builder
	writer := colorprofile.Writer{Forward: &out, Profile: profile}
	_, _ = writer.WriteString(value) // strings.Builder never fails.
	return out.String()
}
