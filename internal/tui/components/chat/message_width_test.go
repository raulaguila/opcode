package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/stretchr/testify/require"
)

func TestMessageFrameUsesExactMarkdownContentWidthWithoutReflow(t *testing.T) {
	if cfg := config.Get(); cfg != nil {
		previous := cfg.TUI.ProseWidth
		t.Cleanup(func() { cfg.TUI.ProseWidth = previous })
		cfg.TUI.ProseWidth = 0
	}
	source := "- **Problema inicial:** " + strings.Repeat("custo de alocação ", 12) + "+173,5% de objetos no Markdown.\n- Unicode ação 漢字 👩‍💻.\n\n```text\n" + strings.Repeat("wide", 30) + "\n```"
	for _, width := range []int{40, 60, 100, 140} {
		view := ansi.Strip(renderMessage(source, false, false, width))
		expected := strings.Split(ansi.Strip(styles.RenderMarkdownBlocks(source, width-1, width-1)), "\n")
		actual := strings.Split(view, "\n")
		require.Len(t, actual, len(expected), "a second wrap must not add rows at width %d", width)
		for i, line := range actual {
			require.True(t, strings.HasPrefix(line, "│"), "every continuation stays inside the pipe")
			require.Equal(t, width, ansi.StringWidth(line))
			require.Equal(t, strings.TrimRight(expected[i], " "), strings.TrimRight(strings.TrimPrefix(line, "│"), " "))
		}
	}
}
