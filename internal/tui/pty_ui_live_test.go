//go:build integration

package tui

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/stretchr/testify/require"
)

// TestLiveOllamaPTYUI exercises the shipped binary, not a model.View snapshot or
// injected Bubble Tea events. Run alone: model generation is intentionally real.
// No inherited credentials, user configuration, or writable real project is used.
func TestLiveOllamaPTYUI(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || os.Getenv("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("requires explicit OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 opt-in")
	}
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux and Bubblewrap")
	}
	_, err := exec.LookPath("bwrap")
	require.NoError(t, err, "Bubblewrap is required; never downgrade the sandbox")
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary), "build opcode and set OPCODE_EVAL_BINARY")
	endpoint := os.Getenv("LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/v1"
	}
	u, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Equal(t, "http", u.Scheme)
	require.Contains(t, []string{"127.0.0.1", "localhost", "::1"}, u.Hostname(), "only local Ollama is allowed")
	model := os.Getenv("OPCODE_LIVE_OLLAMA_MODEL")
	if model == "" {
		model = "llama3.1:8b"
	}
	root := t.TempDir()
	home, workspace, data := filepath.Join(root, "home"), filepath.Join(root, "workspace"), filepath.Join(root, "data")
	for _, directory := range []string{filepath.Join(home, ".opcode"), filepath.Join(workspace, ".opcode"), filepath.Join(workspace, "ui-acceptance-artifacts")} {
		require.NoError(t, os.MkdirAll(directory, 0700))
	}
	fixtures := map[string]string{
		"README.md":      "# IgarapeWatch\n\nAplicativo em Go para coletar métricas de sensores via MQTT, armazenar os dados em SQLite e gerar relatórios pela linha de comando.\n",
		"go.mod":         "module example.test/igarapewatch\n\ngo 1.26\n",
		"main.go":        "package main\n\n// IgarapeWatch collects sensor data in SQLite.\nfunc main() {}\n",
		"interactive.sh": "[ -t 0 ] || exit 42\nprintf started > ui-acceptance-artifacts/command-started\nprintf 'LIVE_READY\\n'\nIFS= read -r value\nprintf 'LIVE_INPUT:%s\\n' \"$value\"\n",
		"lsp-stderr.sh":  "printf ran > ui-acceptance-artifacts/lsp-stderr-ran\nfor ((i=0;i<4096;i++)); do printf 'UI_LSP_SECRET_SENTINEL_%s\\n' \"$i\" >&2; done\nexit 1\n",
	}
	for name, contents := range fixtures {
		require.NoError(t, os.WriteFile(filepath.Join(workspace, name), []byte(contents), 0600))
	}
	modelID := "local." + model
	agents := map[string]any{}
	for _, name := range []string{"coder", "title", "summarizer", "task"} {
		agents[name] = map[string]any{"model": modelID}
	}
	configuration := map[string]any{
		"data": map[string]any{"directory": data}, "runtime": map[string]any{"mode": "ask"},
		"tui":       map[string]any{"language": "en", "sidebar": "hidden"},
		"security":  map[string]any{"sandbox": "bubblewrap"},
		"shell":     map[string]any{"path": "/bin/bash", "args": []string{"--noprofile", "--norc"}},
		"providers": map[string]any{"local": map[string]any{"baseURL": endpoint, "api": "ollama"}},
		"models":    map[string]any{modelID: map[string]any{"provider": "local", "api_model": model, "api": "ollama", "context_window": 16384, "default_max_tokens": 2048}},
		"agents":    agents, "autoCompact": false,
		"lsp": map[string]any{"00-ui-acceptance": map[string]any{"command": "/bin/bash", "args": []string{filepath.Join(workspace, "lsp-stderr.sh")}, "fileTypes": []string{"go"}, "rootMarkers": []string{"go.mod"}}},
	}
	raw, err := json.Marshal(configuration)
	require.NoError(t, err)
	configPath := filepath.Join(home, ".opcode", "config.json")
	require.NoError(t, os.WriteFile(configPath, raw, 0600))
	h := newPTYUIHarness(t, binary, home, workspace)
	h.wait("fresh-project initialization choice", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n") // decline optional memory-file generation; never authorize mutations in Ask.
	h.wait("initialization dismissed", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.wait("initial composer", 20*time.Second, ptyUIHasFooter)
	h.assertComposer()

	// Physical newlines in bracketed paste must never submit the prompt. A long
	// draft grows to eight rows and survives real modal focus changes and resize.
	draft := "draft-01 ação 漢字\ndraft-02\ndraft-03"
	h.paste(draft)
	h.wait("three draft rows", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "draft-03") })
	for i := 4; i <= 8; i++ {
		h.send("\x1b[13;2u") // enhanced Shift+Enter, decoded by the shipped keyboard filter
		h.send(fmt.Sprintf("draft-%02d", i))
	}
	h.wait("eight draft rows", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "draft-01 ação 漢字") && strings.Contains(frame, "draft-08")
	})
	for _, size := range [][2]int{{120, 36}, {180, 40}, {80, 24}} {
		h.resize(size[0], size[1])
		h.wait("resized draft", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "draft-08") })
		h.assertComposer()
	}
	h.send("\x0f") // Ctrl+O opens model selection even with a draft.
	h.wait("model selector", 10*time.Second, func(frame string) bool { return strings.Contains(frame, "Choose model") })
	h.wait("provider catalog", 10*time.Second, func(frame string) bool { return strings.Contains(frame, "Provider catalog") })
	for i := 0; i < 16; i++ {
		h.send("\x1b[B")
	}
	h.wait("model list position", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, " of ") && strings.Contains(frame, "✓ current")
	})
	h.paste("NO_SUCH_UI_ACCEPTANCE_MODEL")
	h.wait("model search filters catalog", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "No models found") })
	h.send("\x15")
	h.paste("llama3.1")
	h.wait("local model search", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Search: llama3.1") && !strings.Contains(frame, "No models found")
	})
	h.resize(120, 36)
	h.send("\x1b")
	h.wait("model selector dismissed", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Choose model") && strings.Contains(frame, "draft-08")
	})
	require.Contains(t, h.frame(), "draft-01 ação 漢字", "modal search must not consume the conversation draft")
	h.clearDraft()
	h.clickMode()
	h.wait("mode chooser", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Work mode") || strings.Contains(frame, "Modo de trabalho")
	})
	h.send("\x1b")
	h.wait("mode chooser dismissed", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Work mode") && !strings.Contains(frame, "Modo de trabalho")
	})
	h.clearDraft()
	t.Log("PTY draft, Unicode paste, three resizes, model catalog/search and mode chooser passed")

	// Exercise the same real LSP NewClient stderr path via an explicit settings
	// probe. This does not depend on a small model obeying diagnostic arguments,
	// nor turn an intentionally unavailable optional LSP into an agent objective.
	h.paste("/integrations")
	h.send("\r")
	h.wait("integration list", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "MCP and language servers") })
	h.send("\r")
	h.wait("LSP form", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Configure LSP") })
	require.Contains(t, h.frame(), "00-ui-acceptance")
	require.Contains(t, h.frame(), "/bin/bash")
	require.Contains(t, h.frame(), "lsp-stderr.sh")
	h.send("\x14") // Ctrl+T requests a connection test, with its own permission.
	h.wait("explicit permission dialog", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Allow (a)") })
	if !strings.Contains(h.frame(), "Tool: test_lsp_connection") {
		h.send("d")
		t.Fatal("unexpected integration permission request; denied")
	}
	_, err = os.Stat(filepath.Join(workspace, "ui-acceptance-artifacts", "lsp-stderr-ran"))
	require.True(t, os.IsNotExist(err), "LSP probe must not start before explicit UI approval")
	h.send("a")
	h.wait("approval dialog dismissed", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "Allow (a)") })
	h.wait("LSP stderr fixture executed", 10*time.Second, func(string) bool {
		_, err := os.Stat(filepath.Join(workspace, "ui-acceptance-artifacts", "lsp-stderr-ran"))
		return err == nil
	})
	h.wait("optional LSP probe failed cleanly", 12*time.Second, func(frame string) bool { return strings.Contains(frame, "Test failed:") })
	require.NotContains(t, h.rawOutput(), "UI_LSP_SECRET_SENTINEL")
	h.send("\x1b")
	h.wait("integration list restored", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "MCP and language servers") })
	h.send("\x1b")
	h.wait("integration dialog dismissed", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "MCP and language servers") && ptyUIHasFooter(frame)
	})
	require.Contains(t, h.frame(), "Ask ▾", "probing an integration must not change the work mode")
	t.Log("separate LSP probe explicitly approved in UI; stderr burst suppressed and Ask mode preserved")

	databaseURI := (&url.URL{Scheme: "file", Path: filepath.Join(data, "opcode.db")}).String() + "?mode=ro"
	conn, err := sql.Open("sqlite3", databaseURI)
	require.NoError(t, err)
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	h.paste(`Explique o projeto atual. Leia README.md e main.go antes de explicar; cite os arquivos e as linhas realmente lidos. Responda em português.`)
	h.send("\r")
	for _, size := range [][2]int{{80, 24}, {120, 36}, {180, 40}} {
		h.resize(size[0], size[1])
		time.Sleep(150 * time.Millisecond)
	}
	sourceCitation := regexp.MustCompile(`(?i)(README\.md|main\.go)(:[1-9][0-9]*|#L[1-9][0-9]*)`)
	h.wait("factual completed overview", 180*time.Second, func(frame string) bool {
		if !ptyUIIdleFooter(frame) {
			return false
		}
		messages := ptyUIMessages(conn)
		if len(messages) == 0 {
			return false
		}
		last := messages[len(messages)-1]
		text := strings.ToLower(last.text())
		return last.Role == "assistant" && last.finished() && strings.Contains(text, "igarapewatch") && strings.Contains(text, "sqlite") && strings.Contains(text, "sensor") && sourceCitation.MatchString(text)
	})
	h.wait("no loading remnants", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "Loading conversation") })
	require.NotContains(t, h.rawOutput(), "UI_LSP_SECRET_SENTINEL", "child stderr must never corrupt the terminal")
	h.assertComposer()
	initialHistory := ptyUIMessages(conn)
	overview := initialHistory[len(initialHistory)-1].text()
	require.NoError(t, os.WriteFile("/tmp/opcode-ui-pty-overview.txt", []byte(overview), 0600))
	t.Logf("overview SHA-256: %x", sha256.Sum256([]byte(overview)))
	t.Log("real Ollama project overview finished with factual content and no raw LSP stderr")

	h.paste("/mode execute")
	h.send("\r")
	h.wait("execute mode", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Execute") })
	h.paste(`Call bash with these arguments: {"command":"bash interactive.sh","pty":true,"timeout":120000}. Wait for the actual tool result. The human will provide terminal input. Do not execute any other command.`)
	h.send("\r")
	h.wait("explicit permission dialog", 120*time.Second, func(frame string) bool { return strings.Contains(frame, "Allow (a)") })
	_, err = os.Stat(filepath.Join(workspace, "ui-acceptance-artifacts", "command-started"))
	require.True(t, os.IsNotExist(err), "command must not execute before UI approval")
	approved := false
	for _, msg := range ptyUIMessages(conn) {
		for _, part := range msg.Parts {
			if part.Type != "tool_call" {
				continue
			}
			var call struct{ Name, Input string }
			_ = json.Unmarshal(part.Data, &call)
			if call.Name != "bash" {
				continue
			}
			var args struct {
				Command string `json:"command"`
				PTY     bool   `json:"pty"`
			}
			if json.Unmarshal([]byte(call.Input), &args) == nil && args.Command == "bash interactive.sh" && args.PTY {
				approved = true
			} else {
				h.send("d")
				t.Fatal("unexpected shell command or non-PTY request was denied")
			}
		}
	}
	if !approved {
		h.send("d")
		t.Fatal("permission request had no matching persisted PTY tool call; denied")
	}
	h.send("a") // approve once, through the actual permission dialog.
	h.wait("approval dialog dismissed", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "Allow (a)") })
	h.wait("live command output before process exit", 15*time.Second, func(frame string) bool { return strings.Contains(frame, "LIVE_READY") })
	t.Log("one exact PTY bash command approved through UI; live output appeared before process exit")
	steering := "Nova instrução: quando o comando atual terminar, responda apenas LIVE_STEERING_OK. Não execute mais ferramentas."
	h.paste(steering)
	h.send("\r")
	h.wait("instruction queued", 5*time.Second, func(frame string) bool {
		return strings.Contains(strings.ToLower(frame), "queued") || strings.Contains(strings.ToLower(frame), "fila")
	})
	h.send("\x10") // Ctrl+P focuses the actual terminal panel.
	h.wait("terminal focus", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Terminal") && strings.Contains(frame, "running")
	})
	h.send("HUMAN_PTY_OK\r")
	h.wait("human input delivered to PTY", 15*time.Second, func(frame string) bool { return strings.Contains(frame, "LIVE_INPUT:HUMAN_PTY_OK") })
	h.send("\x10")
	h.wait("steered response and persisted tool result", 180*time.Second, func(string) bool {
		messages := ptyUIMessages(conn)
		toolAt, instructionAt, finalAt := -1, -1, -1
		for i, msg := range messages {
			if msg.Role == "tool" && strings.Contains(msg.allContent(), "LIVE_INPUT:HUMAN_PTY_OK") {
				toolAt = i
			}
			if msg.Role == "user" && strings.Contains(msg.text(), "LIVE_STEERING_OK") {
				instructionAt = i
			}
			if msg.Role == "assistant" && msg.finished() && strings.Contains(msg.text(), "LIVE_STEERING_OK") {
				finalAt = i
			}
		}
		return toolAt >= 0 && instructionAt > toolAt && finalAt > instructionAt
	})
	h.resize(80, 24)
	h.wait("final composer", 5*time.Second, ptyUIIdleFooter)
	h.assertComposer()
	require.NotContains(t, h.frame(), "Loading conversation")
	require.NotContains(t, h.rawOutput(), "UI_LSP_SECRET_SENTINEL")
	bashCalls := 0
	for _, msg := range ptyUIMessages(conn) {
		for _, part := range msg.Parts {
			var call struct{ Name string }
			if part.Type == "tool_call" && json.Unmarshal(part.Data, &call) == nil && call.Name == "bash" {
				bashCalls++
			}
		}
	}
	require.Equal(t, 1, bashCalls, "steering and resize must never replay an approved command")
	for name, original := range fixtures {
		current, err := os.ReadFile(filepath.Join(workspace, name))
		require.NoError(t, err)
		require.Equal(t, original, string(current), "source fixture must remain unchanged")
	}
	t.Log("real binary PTY: Unicode/paste, 3–8-row composer, resize, model/mode modal focus, Ask evidence, separately approved LSP probe, one approved bash command, live output, terminal input and queued instruction passed")
}

type ptyUIHarness struct {
	t          *testing.T
	terminal   *os.File
	screen     vt10x.Terminal
	mu         sync.Mutex
	raw        strings.Builder
	stderrSeen bool
}

func newPTYUIHarness(t *testing.T, binary, home, workspace string) *ptyUIHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	command := exec.CommandContext(ctx, binary, "--cwd", workspace)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "SHELL=/bin/bash", "LANG=C.UTF-8", "TERM=xterm-256color"}
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 80})
	require.NoError(t, err)
	h := &ptyUIHarness{t: t, terminal: terminal, screen: vt10x.New(vt10x.WithSize(80, 24), vt10x.WithWriter(terminal))}
	done := make(chan struct{})
	go func() {
		defer close(done)
		filter := ptyUIControlFilter{}
		buffer := make([]byte, 8192)
		for {
			n, err := terminal.Read(buffer)
			if n > 0 {
				h.mu.Lock()
				// Bound even a broken renderer's output in a failed test.
				if h.raw.Len() > 2*1024*1024 {
					tail := h.raw.String()[h.raw.Len()-1024*1024:]
					h.raw.Reset()
					h.raw.WriteString(tail)
				}
				h.raw.Write(buffer[:n])
				h.stderrSeen = h.stderrSeen || strings.Contains(h.raw.String(), "UI_LSP_SECRET_SENTINEL")
				_, _ = h.screen.Write(filter.write(buffer[:n]))
				h.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		if t.Failed() {
			_ = os.WriteFile("/tmp/opcode-ui-pty-last.raw", []byte(h.rawOutput()), 0600)
			_ = os.WriteFile("/tmp/opcode-ui-pty-last.txt", []byte(h.frame()), 0600)
		}
		cancel()
		_ = command.Process.Kill()
		_ = terminal.Close()
		_ = command.Wait()
		<-done
	})
	return h
}

func (h *ptyUIHarness) send(value string) {
	h.t.Helper()
	_, err := io.WriteString(h.terminal, value)
	require.NoError(h.t, err)
}
func (h *ptyUIHarness) paste(value string) { h.send("\x1b[200~" + value + "\x1b[201~") }
func (h *ptyUIHarness) clearDraft() {
	// Ctrl+U/Ctrl+End belong to conversation scrolling in this application.
	// The preserved draft cursor is already at its end after dismissing a modal.
	h.send(strings.Repeat("\x7f", 512))
	h.wait("draft cleared", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "draft-") })
}
func (h *ptyUIHarness) frame() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.screen.String()
}
func (h *ptyUIHarness) rawOutput() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stderrSeen {
		return "UI_LSP_SECRET_SENTINEL" + h.raw.String()
	}
	return h.raw.String()
}
func (h *ptyUIHarness) resize(cols, rows int) {
	h.t.Helper()
	h.mu.Lock()
	h.screen.Resize(cols, rows)
	h.mu.Unlock()
	require.NoError(h.t, pty.Setsize(h.terminal, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}))
	h.wait(fmt.Sprintf("terminal resize %dx%d", cols, rows), 5*time.Second, func(frame string) bool {
		lines := strings.Split(frame, "\n")
		return len(lines) >= rows && strings.Count(lines[rows-1], "▾") == 2
	})
}
func (h *ptyUIHarness) clickMode() {
	h.t.Helper()
	for row, line := range strings.Split(h.frame(), "\n") {
		if strings.Count(line, "▾") == 2 {
			// SGR mouse coordinates are one-based; x=3 is inside the mode button.
			h.send(fmt.Sprintf("\x1b[<0;3;%dM\x1b[<0;3;%dm", row+1, row+1))
			return
		}
	}
	h.t.Fatal("mode button is absent from the current footer")
}
func (h *ptyUIHarness) wait(label string, timeout time.Duration, predicate func(string) bool) {
	h.t.Helper()
	until := time.Now().Add(timeout)
	for time.Now().Before(until) {
		frame := h.frame()
		if predicate(frame) {
			return
		}
		if strings.Contains(frame, "Allow (a)") && label != "explicit permission dialog" && label != "approval dialog dismissed" {
			h.send("d")
			h.t.Fatalf("unexpected privileged operation during %s; denied", label)
		}
		time.Sleep(30 * time.Millisecond)
	}
	raw := h.rawOutput()
	if len(raw) > 1600 {
		raw = raw[len(raw)-1600:]
	}
	h.t.Fatalf("timed out waiting for %s; last VT frame:\n%s\nraw output tail: %q", label, h.frame(), raw)
}
func (h *ptyUIHarness) assertComposer() {
	h.t.Helper()
	frame := h.frame()
	prompts, footers := 0, 0
	for _, line := range strings.Split(frame, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			prompts++
		}
		if strings.Count(line, "▾") == 2 {
			footers++
		}
	}
	require.Equal(h.t, 1, prompts, "one composer prompt, no duplicated rows:\n%s", frame)
	require.Equal(h.t, 1, footers, "one footer:\n%s", frame)
}

func ptyUIHasFooter(frame string) bool {
	return ptyUIFooter(frame) != ""
}

func ptyUIIdleFooter(frame string) bool {
	footer := strings.ToLower(ptyUIFooter(frame))
	if footer == "" {
		return false
	}
	for _, problem := range []string{"esc cancels", "source citation", "failed", "blocked", "partial", "error", "limit reached"} {
		if strings.Contains(footer, problem) {
			return false
		}
	}
	return true
}

func ptyUIFooter(frame string) string {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Count(line, "▾") == 2 {
			return line
		}
	}
	return ""
}

// vt10x predates Kitty keyboard negotiation. Filter only mode controls; retain
// every other byte, including Unicode, and independently retain original raw IO.
type ptyUIControlFilter struct{ pending []byte }

func (f *ptyUIControlFilter) write(input []byte) []byte {
	controls := []string{ansi.RequestKittyKeyboard, ansi.PushKittyKeyboard(1), ansi.PopKittyKeyboard(1)}
	var output []byte
	for _, b := range input {
		f.pending = append(f.pending, b)
		for len(f.pending) > 0 {
			prefix := false
			for _, control := range controls {
				if strings.HasPrefix(control, string(f.pending)) {
					prefix = true
					if control == string(f.pending) {
						f.pending = nil
					}
					break
				}
			}
			if prefix {
				break
			}
			output = append(output, f.pending[0])
			f.pending = f.pending[1:]
		}
	}
	return output
}

func TestPTYUIControlFilterFragmentedUnicode(t *testing.T) {
	filter := ptyUIControlFilter{}
	input := ansi.RequestKittyKeyboard + "ação 漢字\x1b[31mred\x1b[0m" + ansi.PushKittyKeyboard(1) + "end" + ansi.PopKittyKeyboard(1)
	var result []byte
	for _, b := range []byte(input) {
		result = append(result, filter.write([]byte{b})...)
	}
	require.Equal(t, "ação 漢字\x1b[31mred\x1b[0mend", string(result))
	require.Empty(t, filter.pending)
}

// An ED0 emitted by the real Bubble Tea renderer erases the current last cell,
// including when printing a full line has left the terminal in pending-wrap.
// This proves why visible footer content must leave the last cell blank.
func TestPTYUIVTLastColumnErase(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		preserved  bool
	}{
		{"visible last cell", strings.Repeat(" ", 79) + "▾", false},
		{"blank last cell", strings.Repeat(" ", 78) + "▾ ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			screen := vt10x.New(vt10x.WithSize(80, 24))
			_, err := screen.Write([]byte("\x1b[24;1H" + tc.line + "\x1b[J"))
			require.NoError(t, err)
			require.Equal(t, tc.preserved, strings.Contains(strings.Split(screen.String(), "\n")[23], "▾"))
		})
	}
}

type ptyUIMessage struct {
	Role  string
	Parts []struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
}

func ptyUIMessages(conn *sql.DB) []ptyUIMessage {
	rows, err := conn.Query("SELECT role, parts FROM messages ORDER BY rowid")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var messages []ptyUIMessage
	for rows.Next() {
		var m ptyUIMessage
		var raw string
		if rows.Scan(&m.Role, &raw) == nil && json.Unmarshal([]byte(raw), &m.Parts) == nil {
			messages = append(messages, m)
		}
	}
	return messages
}
func (m ptyUIMessage) text() string {
	var text strings.Builder
	for _, part := range m.Parts {
		if part.Type == "text" {
			var data struct{ Text string }
			_ = json.Unmarshal(part.Data, &data)
			text.WriteString(data.Text)
		}
	}
	return text.String()
}
func (m ptyUIMessage) allContent() string {
	raw, _ := json.Marshal(m.Parts)
	return string(raw)
}
func (m ptyUIMessage) finished() bool {
	for _, part := range m.Parts {
		if part.Type == "finish" {
			var data struct{ Reason string }
			_ = json.Unmarshal(part.Data, &data)
			if data.Reason != "tool_use" {
				return true
			}
		}
	}
	return false
}
