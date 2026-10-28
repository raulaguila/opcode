package tui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

type keyboardCapture struct{ keys []tea.KeyMsg }

func (*keyboardCapture) Init() tea.Cmd { return nil }
func (*keyboardCapture) View() string  { return "" }
func (m *keyboardCapture) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		m.keys = append(m.keys, key)
		return m, tea.Quit
	}
	return m, nil
}

func captureKey(t *testing.T, input string, keyboard *Keyboard) tea.KeyMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	model := &keyboardCapture{}
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(strings.NewReader(input)), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler(), tea.WithFilter(keyboard.Filter))
	_, err := program.Run()
	require.NoError(t, err)
	require.Len(t, model.keys, 1)
	return model.keys[0]
}

func TestEnhancedKeyboardUsesRealBubbleTeaDecoder(t *testing.T) {
	for sequence, want := range map[string]string{
		"\x1b[13;2u":    "ctrl+j", // CSI-u Shift+Enter.
		"\x1b[27;2;13~": "ctrl+j", // Xterm modifyOtherKeys.
		"\x1b[13;1u":    "enter",
		"\x1b[13;3u":    "alt+enter",
		"\x1b[13;5u":    "ctrl+j",
		"\x1b[99;5u":    "ctrl+c",
		"\x1b[111;5u":   "ctrl+o",
		"\x1b[115;5u":   "ctrl+s",
		"\x1b[27u":      "esc",
		"\x1b[9;2u":     "shift+tab",
		"\x1b[233;3u":   "alt+é",
		"\r":            "enter",
		"\x1b\r":        "alt+enter",
	} {
		t.Run(want+sequence, func(t *testing.T) {
			key := captureKey(t, sequence, NewKeyboard(io.Discard))
			require.Equal(t, want, key.String())
		})
	}
}

func TestKeyboardNegotiatesAndRestoresBeforeLeavingAlternateScreen(t *testing.T) {
	var output bytes.Buffer
	keyboard := NewKeyboard(&output)
	keyboard.probe = true
	keyboard.Filter(nil, tea.WindowSizeMsg{Width: 80, Height: 24})
	keyboard.Filter(nil, tea.WindowSizeMsg{Width: 90, Height: 30})
	require.Equal(t, ansi.RequestKittyKeyboard, output.String())
	key := captureKey(t, "\x1b[?0u\x1b[13;2u", keyboard)
	require.Equal(t, "ctrl+j", key.String())
	require.Contains(t, output.String(), ansi.PushKittyKeyboard(1))
	_, err := keyboard.Write([]byte("\x1b[?1049l"))
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(output.String(), ansi.PopKittyKeyboard(1)+"\x1b[?1049l"))
	require.NoError(t, keyboard.Close())
	require.Equal(t, 1, strings.Count(output.String(), ansi.PopKittyKeyboard(1)), "cleanup must be idempotent")
	keyboard.Filter(nil, tea.WindowSizeMsg{Width: 80, Height: 24})
	require.True(t, strings.HasSuffix(output.String(), ansi.RequestKittyKeyboard), "re-probe after returning from an external editor")
}

func TestKeyboardDoesNotTranslatePastedSequencesOrKeyReleases(t *testing.T) {
	text := "literal \x1b[13;2u content"
	key := captureKey(t, "\x1b[200~"+text+"\x1b[201~", NewKeyboard(io.Discard))
	require.True(t, key.Paste)
	require.Equal(t, text, string(key.Runes))
	key = captureKey(t, "\x1b[13;2:3u\r", NewKeyboard(io.Discard))
	require.Equal(t, "enter", key.String(), "a released key must not insert or submit anything")
	for _, raw := range []string{"\x1b[13;9u", "\x1b[999999999999u", "\x1b[13;0u", "\x1b[55296u", "\x1b[1;2A"} {
		_, ok := enhancedKey(raw)
		require.False(t, ok, raw)
	}
}
