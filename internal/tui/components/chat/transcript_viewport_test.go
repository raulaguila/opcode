package chat

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestTranscriptViewportMatchesBubblesAtEveryOffset(t *testing.T) {
	content := strings.Repeat("wide 漢字 é 👨‍👩‍👧‍👦 content\nshort\n", 30)
	for _, height := range []int{0, 1, 9, 100} {
		want := viewport.New(viewport.WithWidth(18), viewport.WithHeight(height))
		got := newTranscriptViewport(18, height)
		want.SetContent(content)
		got.SetContent(content)
		for offset := 0; offset < 70; offset++ {
			want.SetYOffset(offset)
			got.SetYOffset(offset)
			require.Equal(t, want.YOffset(), got.YOffset)
			require.Equal(t, want.AtBottom(), got.AtBottom())
			require.Equal(t, want.View(), got.View())
		}
	}
}

func TestTranscriptViewportKeysMouseAndShrinkingContent(t *testing.T) {
	want := viewport.New(viewport.WithWidth(20), viewport.WithHeight(7))
	got := newTranscriptViewport(20, 7)
	content := strings.Repeat("text\n", 60)
	want.SetContent(content)
	got.SetContent(content)
	want.GotoBottom()
	got.GotoBottom()
	for _, msg := range []tea.Msg{
		tea.KeyPressMsg{Code: tea.KeyPgUp}, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl},
		tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyDown},
		tea.MouseWheelMsg{Button: tea.MouseWheelUp},
		tea.MouseWheelMsg{Button: tea.MouseWheelDown},
		tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: tea.KeyPgDown},
	} {
		want, _ = want.Update(msg)
		got, _ = got.Update(msg)
		require.Equal(t, want.YOffset(), got.YOffset)
		require.Equal(t, want.View(), got.View())
	}
	want.SetContent("one\ntwo")
	got.SetContent("one\ntwo")
	require.Equal(t, want.YOffset(), got.YOffset)
	require.Equal(t, want.View(), got.View())
}
