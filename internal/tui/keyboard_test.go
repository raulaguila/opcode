package tui

import (
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

type keyboardCapture struct {
	keys     []tea.KeyPressMsg
	pastes   []string
	releases int
	terminal bool
}

func (*keyboardCapture) Init() tea.Cmd { return nil }
func (m *keyboardCapture) View() tea.View {
	view := tea.NewView("")
	view.AltScreen = m.terminal
	view.KeyboardEnhancements.ReportEventTypes = m.terminal
	return view
}
func (m *keyboardCapture) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.KeyPressMsg:
		m.keys = append(m.keys, key)
		return m, tea.Quit
	case tea.PasteMsg:
		m.pastes = append(m.pastes, key.Content)
		return m, tea.Quit
	case tea.KeyReleaseMsg:
		m.releases++
	}
	return m, nil
}

func captureInput(t *testing.T, input string) *keyboardCapture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	model := &keyboardCapture{}
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(strings.NewReader(input)), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	_, err := program.Run()
	require.NoError(t, err)
	return model
}

func captureKey(t *testing.T, input string) tea.KeyPressMsg {
	t.Helper()
	model := captureInput(t, input)
	require.Len(t, model.keys, 1)
	return model.keys[0]
}

func TestEnhancedKeyboardUsesRealBubbleTeaDecoder(t *testing.T) {
	for sequence, want := range map[string]string{
		"\x1b[13;2u":    "shift+enter",
		"\x1b[27;2;13~": "shift+enter",
		"\x1b[13;1u":    "enter",
		"\x1b[13;3u":    "alt+enter",
		"\x1b[13;5u":    "ctrl+enter",
		"\x1b[99;5u":    "ctrl+c",
		"\x1b[111;5u":   "ctrl+o",
		"\x1b[115;5u":   "ctrl+s",
		"\x1b[27u":      "esc",
		"\x1b[9;2u":     "shift+tab",
		"\x1b[233;3u":   "alt+é",
		"\r":            "enter",
		"\x1b\r":        "alt+enter",
		"\x1b[1;3D":     "alt+left",
		"\x1b[1;3C":     "alt+right",
	} {
		t.Run(want+sequence, func(t *testing.T) {
			require.Equal(t, want, captureKey(t, sequence).String())
		})
	}
}

func TestEnhancedProviderArrowsUseRealDecoder(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"\x1b[57350;3u", "alt+left"},
		{"\x1b[57352;2u", "shift+up"},
		{"\x1b[57353;2u", "shift+down"},
		{"\x1b[57356;2u", "shift+home"},
		{"\x1b[57357;6u", "ctrl+shift+end"},
		{"\x1b[57351;3u", "alt+right"},
		{"\x1b[57350;1u", "left"},
		{"\x1b[57351;2u", "shift+right"},
		{"\x1b[57350;5u", "ctrl+left"},
		{"\x1b[57351;8u", "ctrl+alt+shift+right"},
		{"\x1b[57351;3:2u", "alt+right"},
		{"\x1b[57350;3:3u\x1b[57351;3u", "alt+right"},
	} {
		t.Run(tc.want+tc.input, func(t *testing.T) {
			require.Equal(t, tc.want, captureKey(t, tc.input).String())
		})
	}
}

func TestKeyboardDoesNotTranslatePastedSequencesOrKeyReleases(t *testing.T) {
	text := "literal \x1b[13;2u content ação 漢字"
	model := captureInput(t, "\x1b[200~"+text+"\x1b[201~")
	require.Empty(t, model.keys)
	require.Equal(t, []string{text}, model.pastes)
	model = captureInput(t, "\x1b[13;2:3u\r")
	require.Equal(t, 1, model.releases)
	require.Len(t, model.keys, 1)
	require.Equal(t, "enter", model.keys[0].String(), "a released key must not insert or submit anything")
}

func TestKeyboardNegotiatesAndRestoresBeforeLeavingAlternateScreen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native PTY lifecycle is exercised on Unix")
	}
	master, slave, err := pty.Open()
	require.NoError(t, err)
	defer master.Close()
	defer slave.Close()
	require.NoError(t, pty.Setsize(slave, &pty.Winsize{Cols: 80, Rows: 24}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	model := &keyboardCapture{terminal: true}
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(slave), tea.WithOutput(NewTerminalWriter(slave)), tea.WithoutSignalHandler())
	chunks := readKeyboardPTY(master)
	finished := make(chan error, 1)
	stopped := make(chan struct{})
	go func() { _, err := program.Run(); finished <- err; close(stopped) }()
	defer func() { cancel(); <-stopped }()
	var output strings.Builder
	awaitKeyboardQuery(t, ctx, chunks, &output)
	_, err = io.WriteString(master, "\x1b[?1u\x1b[13;2u")
	require.NoError(t, err)
	require.NoError(t, <-finished)
	require.NoError(t, slave.Close())
	for chunk := range chunks {
		output.WriteString(chunk)
	}
	raw := output.String()
	require.Len(t, model.keys, 1)
	require.Equal(t, "shift+enter", model.keys[0].String())
	push := strings.Index(raw, ansi.PushKittyKeyboard(3))
	pop := strings.LastIndex(raw, ansi.PopKittyKeyboard(1))
	leave := strings.LastIndex(raw, "\x1b[?1049l")
	require.GreaterOrEqual(t, push, 0, "native renderer requests enhanced events declared by its view")
	require.Greater(t, pop, push, "native renderer restores enhanced keyboard state")
	require.Greater(t, leave, pop, "restore the active keyboard stack before leaving the alternate screen")
}

func TestTerminalWriterSerializesConcurrentControlSequences(t *testing.T) {
	var output strings.Builder
	writer := NewTerminalWriter(&output)
	const sequence = "\x1b]52;c;Zml4dHVyZQ==\x07"
	var finished sync.WaitGroup
	for range 8 {
		finished.Go(func() {
			for range 50 {
				_, _ = writer.Write([]byte(sequence))
			}
		})
	}
	finished.Wait()
	require.Equal(t, strings.Repeat(sequence, 400), output.String())
}

func readKeyboardPTY(reader io.Reader) <-chan string {
	chunks := make(chan string, 32)
	go func() {
		defer close(chunks)
		buffer := make([]byte, 4096)
		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				chunks <- string(buffer[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return chunks
}

func awaitKeyboardQuery(t *testing.T, ctx context.Context, chunks <-chan string, output *strings.Builder) {
	t.Helper()
	for !strings.Contains(output.String(), ansi.RequestKittyKeyboard) {
		select {
		case chunk, ok := <-chunks:
			require.True(t, ok, "terminal ended before the keyboard query")
			output.WriteString(chunk)
		case <-ctx.Done():
			t.Fatalf("native keyboard query missing: %q", output.String())
		}
	}
}
