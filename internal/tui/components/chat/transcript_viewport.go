package chat

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Keep transcript line references and global scroll coordinates locally. Bubbles
// receives only the visible window, avoiding its full-content split/width scan.
type transcriptViewport struct {
	viewport.Model
	lines []string
}

func newTranscriptViewport(width, height int) transcriptViewport {
	return transcriptViewport{Model: viewport.New(width, height)}
}

func (v *transcriptViewport) SetContent(content string) {
	v.SetLines(strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n"))
}

func (v *transcriptViewport) SetLines(lines []string) {
	v.lines = lines
	if v.YOffset > len(lines)-1 {
		v.GotoBottom()
	}
}

func (v transcriptViewport) maxYOffset() int {
	return max(0, len(v.lines)-v.Height+v.Style.GetVerticalFrameSize())
}

func (v transcriptViewport) AtBottom() bool      { return v.YOffset >= v.maxYOffset() }
func (v transcriptViewport) TotalLineCount() int { return len(v.lines) }

func (v *transcriptViewport) SetYOffset(offset int) {
	v.YOffset = max(0, min(offset, v.maxYOffset()))
}

func (v transcriptViewport) visibleLines() []string {
	start := min(max(0, v.YOffset), len(v.lines))
	end := min(max(start, start+v.Height-v.Style.GetVerticalFrameSize()), len(v.lines))
	return v.lines[start:end]
}

func (v *transcriptViewport) GotoTop() []string {
	v.SetYOffset(0)
	return v.visibleLines()
}

func (v *transcriptViewport) GotoBottom() []string {
	v.SetYOffset(v.maxYOffset())
	return v.visibleLines()
}

func (v *transcriptViewport) ScrollUp(lines int) []string {
	v.SetYOffset(v.YOffset - lines)
	return v.visibleLines()
}

func (v *transcriptViewport) ScrollDown(lines int) []string {
	v.SetYOffset(v.YOffset + lines)
	return v.visibleLines()
}

func (v transcriptViewport) View() string {
	window := v.Model
	window.YOffset = 0
	window.SetContent(strings.Join(v.visibleLines(), "\n"))
	return window.View()
}

func (v transcriptViewport) Update(msg tea.Msg) (transcriptViewport, tea.Cmd) {
	v.SetYOffset(v.YOffset + v.scrollDelta(msg))
	return v, nil
}

func (v transcriptViewport) scrollDelta(msg tea.Msg) int {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return v.keyScroll(msg)
	case tea.MouseMsg:
		return v.mouseScroll(msg)
	}
	return 0
}

func (v transcriptViewport) keyScroll(msg tea.KeyMsg) int {
	switch {
	case key.Matches(msg, v.KeyMap.PageDown):
		return v.Height
	case key.Matches(msg, v.KeyMap.PageUp):
		return -v.Height
	case key.Matches(msg, v.KeyMap.HalfPageDown):
		return v.Height / 2
	case key.Matches(msg, v.KeyMap.HalfPageUp):
		return -v.Height / 2
	case key.Matches(msg, v.KeyMap.Down):
		return 1
	case key.Matches(msg, v.KeyMap.Up):
		return -1
	}
	return 0
}

func (v transcriptViewport) mouseScroll(msg tea.MouseMsg) int {
	if !v.MouseWheelEnabled || msg.Action != tea.MouseActionPress || msg.Shift {
		return 0
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -v.MouseWheelDelta
	case tea.MouseButtonWheelDown:
		return v.MouseWheelDelta
	}
	return 0
}
