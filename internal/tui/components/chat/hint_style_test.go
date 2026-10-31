package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func chatHintGlyph(t *testing.T, view, text string) vt10x.Glyph {
	t.Helper()
	screen := vt10x.New(vt10x.WithSize(180, 80))
	_, err := screen.Write([]byte(strings.ReplaceAll(styles.TerminalOutput(view), "\n", "\r\n")))
	require.NoError(t, err)
	for y, row := range strings.Split(ansi.Strip(view), "\n") {
		if before, _, found := strings.Cut(row, text); found {
			return screen.Cell(ansi.StringWidth(before), y)
		}
	}
	t.Fatalf("missing legend %q in %s", text, ansi.Strip(view))
	return vt10x.Glyph{}
}

func TestChatHintsUseTheirActualBackgroundAndKeepNormalAttributes(t *testing.T) {
	profile, name, dark := theme.ColorProfile(), theme.CurrentThemeName(), theme.HasDarkBackground()
	t.Cleanup(func() { theme.SetColorProfile(profile); _ = theme.PreviewTheme(name); theme.SetHasDarkBackground(dark) })
	for _, name := range []string{"opcode", "onedark", "tokyonight"} {
		require.NoError(t, theme.PreviewTheme(name))
		for _, dark := range []bool{false, true} {
			theme.SetHasDarkBackground(dark)
			for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.Ascii} {
				theme.SetColorProfile(profile)
				assertChatHintSurfaces(t)
			}
		}
	}
}

func assertChatHintSurfaces(t *testing.T) {
	t.Helper()
	palette := theme.CurrentTheme()
	root := chatHintGlyph(t, styles.HintOn(palette.Background()).Render("root"), "root")
	m := &messagesCmp{width: 100, toggled: map[string]bool{"group": true}}
	r := turnRenderer{messages: m, base: styles.BaseStyle(), theme: palette}
	item := message.Message{ID: "answer", Parts: []message.ContentPart{message.ReasoningContent{Thinking: "Raw reasoning stays unchanged"}}}
	r.renderActions(item)
	r.renderReasoningDetails(item)
	r.renderRevisionToggle("previous", "Older version")
	r.renderProcess(turnGroup{key: "group", messages: []message.Message{{Parts: []message.ContentPart{message.ToolCall{Name: "view"}}}}}, true)
	view := strings.Join(r.rendered, "\n")
	for _, text := range []string{"Drag to select", "Model reasoning", "Older version", "Process"} {
		actual := chatHintGlyph(t, view, text)
		require.Equal(t, root.FG, actual.FG)
		require.Equal(t, root.BG, actual.BG)
		require.Zero(t, actual.Mode&23)
	}
	body := chatHintGlyph(t, styles.BaseStyle().Render("body"), "body")
	require.Equal(t, body.FG, chatHintGlyph(t, view, "working").FG, "live status remains primary")
	for _, failed := range []bool{false, true} {
		result := message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "read", IsError: failed}}}
		completed := turnRenderer{messages: m, base: styles.BaseStyle(), theme: palette}
		completed.renderProcess(turnGroup{key: "done", messages: []message.Message{result}}, false)
		completedView := strings.Join(completed.rendered, "\n")
		want := root.FG
		if failed {
			want = body.FG
			require.Equal(t, body.FG, chatHintGlyph(t, completedView, "failure(s)").FG, "recorded failures remain primary")
		}
		require.Equal(t, want, chatHintGlyph(t, completedView, "finished").FG, "successful completion stays secondary without demoting failures")
	}
	require.Equal(t, root.FG, chatHintGlyph(t, m.navigationHint(), "Arrows move").FG)
	m.find = transcriptFind{open: true, query: "query"}
	require.Equal(t, root.FG, chatHintGlyph(t, m.navigationHint(), "Find:").FG)
	editor := slashEditor()
	placeholder := editor.textarea.Styles().Focused.Placeholder.Render("Placeholder")
	require.Equal(t, root.FG, chatHintGlyph(t, placeholder, "Placeholder").FG)
	enterText(editor, "/")
	panel := chatHintGlyph(t, styles.PanelHint().Render("panel"), "panel")
	slash := chatHintGlyph(t, editor.SlashMenuView(76, 11), "↑/↓ choose")
	require.Equal(t, panel.FG, slash.FG)
	require.Equal(t, panel.BG, slash.BG)
	editor.attachments = []message.Attachment{{FileName: "report.txt"}}
	chip := chatHintGlyph(t, editor.attachmentsContentWidth(100), "report.txt")
	primary := styles.BaseStyle().Background(palette.BackgroundSecondary()).Foreground(styles.ReadableForeground(palette.Text(), palette.BackgroundSecondary())).Render("primary")
	require.Equal(t, chatHintGlyph(t, primary, "primary").FG, chip.FG, "filenames remain primary content")
}
