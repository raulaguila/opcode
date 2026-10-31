package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestRootNoColorPreservesSelectionHighlight(t *testing.T) {
	previous := theme.ColorProfile()
	theme.SetColorProfile(colorprofile.Ascii)
	t.Cleanup(func() { theme.SetColorProfile(previous) })
	// This is the color-free reverse attribute emitted by transcript selection.
	p := &focusPageProbe{content: "before \x1b[7mselected text\x1b[27m after"}
	status, _ := core.NewStatusCmp(nil).Update(tea.WindowSizeMsg{Width: 80})
	a := appModel{width: 80, height: 23, currentPage: page.ChatPage,
		pages: map[page.PageID]util.Model{page.ChatPage: p}, status: status.(core.StatusCmp)}
	require.Contains(t, a.View().Content, "\x1b[7mselected text\x1b[27m", "the final output boundary must preserve monochrome selection")
}
