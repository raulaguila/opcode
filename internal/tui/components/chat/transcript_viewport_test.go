package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func TestTranscriptViewportMatchesBubblesAtEveryOffset(t *testing.T) {
	content := strings.Repeat("wide 漢字 é 👨‍👩‍👧‍👦 content\nshort\n", 30)
	for _, height := range []int{0, 1, 9, 100} {
		want := viewport.New(18, height)
		got := newTranscriptViewport(18, height)
		want.SetContent(content)
		got.SetContent(content)
		for offset := 0; offset < 70; offset++ {
			want.SetYOffset(offset)
			got.SetYOffset(offset)
			require.Equal(t, want.YOffset, got.YOffset)
			require.Equal(t, want.AtBottom(), got.AtBottom())
			require.Equal(t, want.View(), got.View())
		}
	}
}

func TestTranscriptViewportKeysMouseAndShrinkingContent(t *testing.T) {
	want := viewport.New(20, 7)
	got := newTranscriptViewport(20, 7)
	content := strings.Repeat("text\n", 60)
	want.SetContent(content)
	got.SetContent(content)
	want.GotoBottom()
	got.GotoBottom()
	for _, msg := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyPgUp}, tea.KeyMsg{Type: tea.KeyCtrlU},
		tea.KeyMsg{Type: tea.KeyUp}, tea.KeyMsg{Type: tea.KeyDown},
		tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress},
		tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
		tea.KeyMsg{Type: tea.KeyCtrlD}, tea.KeyMsg{Type: tea.KeyPgDown},
	} {
		want, _ = want.Update(msg)
		got, _ = got.Update(msg)
		require.Equal(t, want.YOffset, got.YOffset)
		require.Equal(t, want.View(), got.View())
	}
	want.SetContent("one\ntwo")
	got.SetContent("one\ntwo")
	require.Equal(t, want.YOffset, got.YOffset)
	require.Equal(t, want.View(), got.View())
}
