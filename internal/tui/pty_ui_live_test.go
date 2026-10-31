//go:build integration

package tui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/raulaguila/opcode/internal/clipboard"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

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
	testLiveOllamaPTYUIOPCODELIVEOLLAMA(t)
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
	prepareLiveOllamaPTY(t, endpoint, model, 16384)
	root := t.TempDir()
	home, workspace, data := filepath.Join(root, "home"), filepath.Join(root, "workspace"), filepath.Join(root, "data")
	for _, directory := range []string{filepath.Join(home, ".opcode"), filepath.Join(workspace, ".opcode"), filepath.Join(workspace, ".cache/ui-acceptance-artifacts")} {
		require.NoError(t, os.MkdirAll(directory, 0700))
	}
	// Instrumentation is derived cache, not a source edit requiring project checks.
	// Every source fixture is still compared byte-for-byte at the end.
	fixtures := map[string]string{
		"README.md":            "# IgarapeWatch\n\nAplicativo em Go para coletar métricas de sensores via MQTT, armazenar os dados em SQLite e gerar relatórios pela linha de comando.\n",
		"go.mod":               "module example.test/igarapewatch\n\ngo 1.26\n",
		"main.go":              "package main\n\n// IgarapeWatch collects sensor data in SQLite.\nfunc main() {}\n",
		"service.go":           "package main\n\n// Application service receives sensor samples for IgarapeWatch reports.\nfunc collectSamples() string { return \"SQLite\" }\n",
		"context_bootstrap.go": "package main\nfunc bootstrap() string { return \"application service handler run execute DECOY_QUERY_LITERAL\" }\n",
		"interactive.sh":       "[ -t 0 ] || exit 42\nprintf started > .cache/ui-acceptance-artifacts/command-started\nprintf 'LIVE_READY ação 漢字\\n'\nIFS= read -r value\nprintf 'LIVE_INPUT:%s\\n' \"$value\"\n",
		"lsp-stderr.sh":        "printf ran > .cache/ui-acceptance-artifacts/lsp-stderr-ran\nfor ((i=0;i<4096;i++)); do printf 'UI_LSP_SECRET_SENTINEL_%s\\n' \"$i\" >&2; done\nexit 1\n",
	}
	for name, contents := range fixtures {
		require.NoError(t, os.WriteFile(filepath.Join(workspace, name), []byte(contents), 0600))
	}
	modelID := "local." + model
	agents := map[string]any{}
	for _, name := range []string{"coder", "title", "summarizer", "task"} {
		agents[name] = map[string]any{"model": modelID}
	}
	// A second loopback connection has a distinct catalog, so navigation must
	// replace the model list rather than merely changing the provider title.
	var catalogAvailable atomic.Bool
	catalogAvailable.Store(true)
	catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { testLiveOllamaPTYUICallback(&catalogAvailable, w, r) }))
	defer catalog.Close()
	configuration := map[string]any{
		"data": map[string]any{"directory": data}, "runtime": map[string]any{"mode": "ask"},
		"tui":       map[string]any{"language": "en", "sidebar": "hidden"},
		"security":  map[string]any{"sandbox": "bubblewrap"},
		"shell":     map[string]any{"path": "/bin/bash", "args": []string{"--noprofile", "--norc"}},
		"providers": map[string]any{"local": map[string]any{"baseURL": endpoint, "api": "ollama"}},
		"models":    map[string]any{modelID: map[string]any{"provider": "local", "api_model": model, "api": "ollama", "context_window": 16384, "default_max_tokens": 2048}},
		"agents":    agents, "autoCompact": false,
		"providerProfiles": map[string]any{"navigation": map[string]any{"name": "Navigation fixture", "backend": "local", "baseURL": catalog.URL + "/v1", "api": "chat_completions"}},
		"lsp":              map[string]any{"00-ui-acceptance": map[string]any{"command": "/bin/bash", "args": []string{filepath.Join(workspace, "lsp-stderr.sh")}, "fileTypes": []string{"go"}, "rootMarkers": []string{"go.mod"}}},
	}
	raw, err := json.Marshal(configuration)
	require.NoError(t, err)
	configPath := filepath.Join(home, ".opcode", "config.json")
	require.NoError(t, os.WriteFile(configPath, raw, 0600))
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 80, 24, "SSH_CONNECTION=isolated-acceptance")
	h.resize(60, 20)
	h.wait("fresh-project initialization choice", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n") // decline optional memory-file generation; never authorize mutations in Ask.
	h.wait("initialization dismissed", 5*time.Second, testLiveOllamaPTYUIInitializationDismissed)
	h.wait("initial composer", 20*time.Second, ptyUIHasFooter)
	h.assertComposer()
	h.resize(80, 24)

	// Physical newlines in bracketed paste must never submit the prompt. A long
	// draft grows to eight rows and survives real modal focus changes and resize.
	draft := "draft-01 ação 漢字\ndraft-02\ndraft-03"
	h.paste(draft)
	h.wait("three draft rows", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "draft-03") })
	for i := 4; i <= 8; i++ {
		h.send("\x1b[13;2u") // enhanced Shift+Enter, decoded by the shipped keyboard filter
		h.send(fmt.Sprintf("draft-%02d", i))
	}
	h.wait("eight draft rows", 5*time.Second, testLiveOllamaPTYUIEightDraftRows)
	for _, size := range [][2]int{{120, 36}, {180, 40}, {80, 24}} {
		h.resize(size[0], size[1])
		h.wait("resized draft", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "draft-08") })
		h.assertComposer()
	}
	h.send("\x0f") // Ctrl+O opens model selection even with a draft.
	h.wait("model selector", 10*time.Second, func(frame string) bool { return strings.Contains(frame, "Choose model") })
	h.wait("provider catalog", 10*time.Second, func(frame string) bool { return strings.Contains(frame, "Provider catalog") })
	testLiveOllamaPTYUIStruct(h)
	h.send("\t")
	h.wait("catalog before outage", 10*time.Second, func(frame string) bool { return strings.Contains(frame, "UI_NAVIGATION_ONLY_MODEL") })
	catalogAvailable.Store(false)
	h.send("\x12") // Ctrl+R refresh against a failing provider.
	h.wait("failed provider stays selected with empty list", 10*time.Second, testLiveOllamaPTYUIFailedProviderStaysSelectedWithEmptyList)
	catalogAvailable.Store(true)
	h.send("\x12")
	h.wait("provider retry recovers catalog", 10*time.Second, testLiveOllamaPTYUIProviderRetryRecoversCatalog)
	h.send("\x1b[Z")
	h.wait("return to Ollama after outage", 10*time.Second, testLiveOllamaPTYUIReturnToOllamaAfterOutage)
	for range 16 {
		h.send("\x1b[B")
	}
	h.wait("model list position", 5*time.Second, testLiveOllamaPTYUIModelListPosition)
	h.paste("NO_SUCH_UI_ACCEPTANCE_MODEL")
	h.wait("model search filters catalog", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "No models found") })
	h.send("\x15")
	h.paste("llama3.1")
	h.wait("local model search", 5*time.Second, testLiveOllamaPTYUILocalModelSearch)
	h.resize(120, 36)
	h.send("\x1b")
	h.wait("model selector dismissed", 5*time.Second, testLiveOllamaPTYUIModelSelectorDismissed)
	require.Contains(t, h.frame(), "draft-01 ação 漢字", "modal search must not consume the conversation draft")
	h.clearDraft()
	h.paste("/settings")
	h.send("\r")
	h.wait("connection list before deletion", 5*time.Second, testLiveOllamaPTYUIConnectionListBeforeDeletion)
	h.send("d")
	h.wait("delete connection confirmation", 5*time.Second, testLiveOllamaPTYUIDeleteConnectionConfirmation)
	h.send("\r") // Confirmation defaults to Cancel.
	h.wait("cancel preserves connection", 5*time.Second, testLiveOllamaPTYUICancelPreservesConnection)
	h.send("d")
	h.wait("second delete confirmation", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Delete connection?") })
	h.send("\x1b[C\r") // Explicitly select Delete, then confirm.
	h.wait("deleted connection disappears", 5*time.Second, testLiveOllamaPTYUIDeletedConnectionDisappears)
	h.send("\x1b")
	h.wait("connection list dismissed", 5*time.Second, testLiveOllamaPTYUIConnectionListDismissed)
	t.Log("PTY failed catalog is empty, retry recovers, delete defaults to Cancel and explicit deletion removes connection")
	h.clickMode()
	h.wait("mode chooser", 5*time.Second, testLiveOllamaPTYUIModeChooser)
	h.send("\x1b")
	h.wait("mode chooser dismissed", 5*time.Second, testLiveOllamaPTYUIModeChooserDismissed)
	h.clearDraft()
	t.Log("PTY draft, Unicode paste, three resizes, model catalog/search and mode chooser passed")
	h.paste("edit-key-check")
	h.send("\x15")
	h.wait("Ctrl+U edits the input", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "edit-key-check") })
	h.paste("/task")
	h.send("\r")
	h.wait("task overview", 5*time.Second, testLiveOllamaPTYUITaskOverview)
	h.send("\x1b")
	h.wait("overview dismissed", 5*time.Second, testLiveOllamaPTYUIOverviewDismissed)
	h.paste("/context")
	h.send("\r")
	h.wait("unified context panel", 10*time.Second, testLiveOllamaPTYUIUnifiedContextPanel)
	h.send("\x1b")
	h.wait("context dismissed", 5*time.Second, testLiveOllamaPTYUIContextDismissed)

	// Exercise the same real LSP NewClient stderr path via an explicit settings
	// probe. This does not depend on a small model obeying diagnostic arguments,
	// nor turn an intentionally unavailable optional LSP into an agent objective.
	h.paste("/integrations")
	h.send("\r")
	h.wait("integration list", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "MCP and language servers") })
	h.send("\r")
	h.wait("complete LSP form", 5*time.Second, testLiveOllamaPTYUILSPFormReady)
	require.Contains(t, h.frame(), "00-ui-acceptance")
	require.Contains(t, h.frame(), "/bin/bash")
	h.send("\t\t\r") // Open the argument list and inspect the end of a long path.
	h.wait("argument list", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Arguments · one per row") })
	h.send("\x1b[F")
	h.wait("complete argument target", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "lsp-stderr.sh") })
	h.send("\x1b")
	h.wait("LSP form restored", 5*time.Second, testLiveOllamaPTYUILSPFormRestored)
	h.send("\x14") // Ctrl+T requests a connection test, with its own permission.
	h.wait("explicit permission dialog", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Allow (a)") })
	if !strings.Contains(h.frame(), "Tool: test_lsp_connection") {
		h.send("d")
		t.Fatal("unexpected integration permission request; denied")
	}
	_, err = os.Stat(filepath.Join(workspace, ".cache/ui-acceptance-artifacts", "lsp-stderr-ran"))
	require.True(t, os.IsNotExist(err), "LSP probe must not start before explicit UI approval")
	require.Contains(t, h.frame(), "/bin/bash")
	require.Contains(t, h.frame(), "lsp-stderr.sh")
	testLiveOllamaPTYUIState(t, workspace, h)
	h.clickLabel("Allow (a)")
	h.wait("approval dialog dismissed", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "Allow (a)") })
	h.wait("LSP stderr fixture executed", 10*time.Second, func(string) bool {
		_, err := os.Stat(filepath.Join(workspace, ".cache/ui-acceptance-artifacts", "lsp-stderr-ran"))
		return err == nil
	})
	h.wait("optional LSP probe failed cleanly", 12*time.Second, func(frame string) bool { return strings.Contains(frame, "Test failed:") })
	require.NotContains(t, h.rawOutput(), "UI_LSP_SECRET_SENTINEL")
	h.send("\x1b")
	h.wait("integration list restored", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "MCP and language servers") })
	h.send("\x1b")
	h.wait("integration dialog dismissed", 5*time.Second, testLiveOllamaPTYUIIntegrationDialogDismissed)
	require.Contains(t, h.frame(), "Ask ▾", "probing an integration must not change the work mode")
	t.Log("separate LSP probe explicitly approved in UI; stderr burst suppressed and Ask mode preserved")

	databaseURI := (&url.URL{Scheme: "file", Path: filepath.Join(data, "opcode.db")}).String() + "?mode=ro"
	conn, err := sql.Open("sqlite3", databaseURI)
	require.NoError(t, err)
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	h.paste(`o que pode me falar sobre esse projeto?`)
	h.send("\r")
	for _, size := range [][2]int{{80, 24}, {120, 36}, {180, 40}} {
		h.resize(size[0], size[1])
		time.Sleep(150 * time.Millisecond)
	}
	sourceCitation := regexp.MustCompile(`(?i)(README\.md|main\.go)(:[1-9][0-9]*|#L[1-9][0-9]*)`)
	h.wait("factual completed overview", 180*time.Second, func(frame string) bool {
		return testLiveOllamaPTYUIFactualCompletedOverview(&conn, &sourceCitation, frame)
	})
	require.NotContains(t, h.frame(), "Application validation:", "internal repairs must stay in process details")
	h.wait("no loading remnants", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "Loading conversation") })
	h.wait("context and measured provider speed in footer", 5*time.Second, testLiveOllamaPTYUIContextAndMeasuredProviderSpeedInFooter)
	require.NotContains(t, h.rawOutput(), "UI_LSP_SECRET_SENTINEL", "child stderr must never corrupt the terminal")
	h.assertComposer()
	initialHistory := ptyUIMessages(conn)
	assertPTYOverviewPreparation(t, initialHistory)
	overview := initialHistory[len(initialHistory)-1].text()
	assertPTYOverviewQuality(t, overview)
	require.NoError(t, os.WriteFile("/tmp/opcode-ui-pty-overview.txt", []byte(overview), 0600))
	t.Logf("overview SHA-256: %x", sha256.Sum256([]byte(overview)))
	t.Log("real Ollama project overview finished with factual content and no raw LSP stderr")
	h.send("\x19")
	h.wait("response actions for explicit SSH copy", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Copy response") })
	h.send("\r")
	expectedOSC, copyErr := clipboard.OSC52(overview)
	require.NoError(t, copyErr)
	h.wait("OSC52 emitted while alternate screen is active", 5*time.Second, func(frame string) bool {
		return testLiveOllamaPTYUIOSC52EmittedWhileAlternateScreenIsActive(&h, &expectedOSC, frame)
	})
	h.send("\x1b")
	h.wait("SSH copy panel dismissed", 5*time.Second, testLiveOllamaPTYUISSHCopyPanelDismissed)

	// A native model question must reach the human, accept a real answer, and
	// resume once. Encoded choices are additionally injected in runtime tests.
	beforeQuestion := len(ptyUIMessages(conn))
	h.paste(`Use ask_user_question exactly once to ask "Qual área você quer revisar?" with choices ["UI","Contexto"]. Wait for the human answer. Then reply with exactly "Resposta recebida: " followed by the chosen option. Do not call other tools or commands.`)
	h.send("\r")
	h.wait("real model question with selectable choices", 120*time.Second, testLiveOllamaPTYUIRealModelQuestionWithSelectableChoices)
	h.send("\x1b[B\r")
	h.wait("human question answer resumes generation", 120*time.Second, func(frame string) bool {
		return testLiveOllamaPTYUIHumanQuestionAnswerResumesGeneration(&conn, &beforeQuestion, frame)
	})
	questions := 0
	testLiveOllamaPTYUIPtyUIMessagesConnBeforeQuestion(t, conn, beforeQuestion, &questions)
	require.Equal(t, 1, questions, "no failed question or repeated question call")

	h.paste("/mode execute")
	h.send("\r")
	h.wait("execute mode and acknowledged command submission", 5*time.Second, testLiveOllamaPTYUIExecuteModeAndAcknowledgedCommandSubmission)
	h.paste(`Call bash with these arguments: {"command":"bash interactive.sh","pty":true,"timeout":120000}. Wait for the actual tool result. The human will provide terminal input. Do not execute any other command.`)
	h.send("\r")
	h.wait("explicit permission dialog", 120*time.Second, func(frame string) bool { return strings.Contains(frame, "Allow (a)") })
	_, err = os.Stat(filepath.Join(workspace, ".cache/ui-acceptance-artifacts", "command-started"))
	require.True(t, os.IsNotExist(err), "command must not execute before UI approval")
	approved := false
	testLiveOllamaPTYUIPtyUIMessagesConn(t, h, conn, &approved)
	if !approved {
		h.send("d")
		t.Fatal("permission request had no matching persisted PTY tool call; denied")
	}
	h.send("a") // approve once, through the actual permission dialog.
	h.wait("approval dialog dismissed", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "Allow (a)") })
	h.wait("live command output before process exit", 15*time.Second, func(frame string) bool { return strings.Contains(frame, "LIVE_READY ação 漢字") })
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
	h.send("HUMAN_PTY_OK ação 漢字\r")
	h.wait("human input delivered to PTY", 15*time.Second, func(frame string) bool { return strings.Contains(frame, "LIVE_INPUT:HUMAN_PTY_OK ação 漢字") })
	h.send("\x10")
	h.wait("steered response and persisted tool result", 180*time.Second, func(callbackArg0 string) bool {
		return testLiveOllamaPTYUISteeredResponseAndPersistedToolResult(&conn, callbackArg0)
	})
	h.resize(80, 24)
	h.wait("final composer", 5*time.Second, ptyUIIdleFooter)
	h.assertComposer()
	require.NotContains(t, h.frame(), "Loading conversation")
	require.NotContains(t, h.rawOutput(), "UI_LSP_SECRET_SENTINEL")
	bashCalls := 0
	testLiveOllamaPTYUIPtyUIMessagesConnCase(conn, &bashCalls)
	require.Equal(t, 1, bashCalls, "steering and resize must never replay an approved command")
	for name, original := range fixtures {
		current, err := os.ReadFile(filepath.Join(workspace, name))
		require.NoError(t, err)
		require.Equal(t, original, string(current), "source fixture must remain unchanged")
	}
	t.Log("real binary PTY: Unicode/paste, 3–8-row composer, resize, model/mode modal focus, Ask evidence, separately approved LSP probe, one approved bash command, live output, terminal input and queued instruction passed")
}

func assertPTYOverviewQuality(t *testing.T, overview string) {
	t.Helper()
	lower := strings.ToLower(overview)
	require.NotContains(t, lower, "bootstrap", "an overview must explain the product, not the context helper")
	require.NotContains(t, lower, "readindex")
	require.NotRegexp(t, `(?m)^\s*[\x60]?[\p{L}\p{N}_./-]+\.(?:md|go|py|ts):[1-9][0-9]*(?:-[1-9][0-9]*)?[\x60]?\s*$`, overview, "citations belong beside claims, not in isolated footers")
	require.Regexp(t, `(?i)(analisei|analisad|inspe[cç]|consultei|examin|leitura|arquivos lidos|basead|não (executei|avaliei|testei)|sem (executar|avaliar|testar)|inspected|reviewed|based on|not (run|tested|evaluated))`, overview, "state the actual inspection scope or a concrete evaluation limit")
	require.NotRegexp(t, `(?i)\b(robust[oa]|production-ready)\b`, overview, "do not present an unmeasured quality judgment as a project fact")
}

func assertPTYOverviewPreparation(t *testing.T, history []ptyUIMessage) {
	t.Helper()
	var searched, readImplementation bool
	for _, item := range history {
		for _, part := range item.Parts {
			switch part.Type {
			case "tool_call":
				var call struct{ Name string }
				require.NoError(t, json.Unmarshal(part.Data, &call))
				searched = searched || call.Name == "search_code_index"
			case "tool_result":
				var result struct {
					Name    string
					Content string
					IsError bool `json:"is_error"`
				}
				require.NoError(t, json.Unmarshal(part.Data, &result))
				require.NotContains(t, result.Content, "DECOY_QUERY_LITERAL", "overview preparation must filter incidental query literals before exposing context")
				readImplementation = readImplementation || (result.Name == "view" && !result.IsError && strings.Contains(result.Content, "service.go") && strings.Contains(result.Content, "collectSamples"))
			}
		}
	}
	require.True(t, searched, "a generic overview must inspect beyond the fixed basic files")
	require.True(t, readImplementation, "index previews must be followed by a real implementation read")
}

type ptyUIHarness struct {
	command    *exec.Cmd
	exited     <-chan struct{}
	t          *testing.T
	terminal   *os.File
	screen     vt10x.Terminal
	mu         sync.Mutex
	raw        strings.Builder
	stderrSeen bool
}

func newPTYUIHarnessAtSize(t *testing.T, binary, home, workspace string, cols, rows int, extraEnv ...string) *ptyUIHarness {
	return newPTYUIHarnessWithArgs(t, binary, home, workspace, cols, rows, nil, extraEnv...)
}

func newPTYUIHarnessWithArgs(t *testing.T, binary, home, workspace string, cols, rows int, args []string, extraEnv ...string) *ptyUIHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	command := exec.CommandContext(ctx, binary, append([]string{"--cwd", workspace}, args...)...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "SHELL=/bin/bash", "LANG=C.UTF-8", "TERM=xterm-256color"}
	command.Env = append(command.Env, extraEnv...)
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	require.NoError(t, err)
	h := &ptyUIHarness{t: t, terminal: terminal, screen: vt10x.New(vt10x.WithSize(cols, rows), vt10x.WithWriter(terminal))}
	done := make(chan struct{})
	h.command, h.exited = command, done
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
	// Ctrl+End edits the composer. Backspace clears all rows, including Unicode.
	h.send("\x1b[1;5F")
	h.send(strings.Repeat("\x7f", 512))
	h.wait("draft cleared", 5*time.Second, ptyUIComposerEmpty)
}
func ptyUIComposerEmpty(frame string) bool {
	if !ptyUIHasFooter(frame) {
		return false
	}
	lines := strings.Split(frame, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if strings.Count(lines[index], "▾") == 2 {
			lines = lines[:index]
			break
		}
	}
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if strings.HasPrefix(line, ">") {
			return line == ">"
		}
		if line != "" {
			return false
		}
	}
	return false
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
	for line := range strings.SplitSeq(frame, "\n") {
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
	for line := range strings.SplitSeq(frame, "\n") {
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
		writeFPending(f, controls, &output)
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

func (h *ptyUIHarness) clickLabel(label string) {
	h.t.Helper()
	x, y := -1, -1
	waitLabel := "stable click target: " + label
	if label == "Allow (a)" {
		waitLabel = "explicit permission dialog"
	}
	// A VT paint arrives in fragments; do not click a label while its row moves.
	h.wait(waitLabel, 5*time.Second, func(frame string) bool {
		for row, line := range strings.Split(frame, "\n") {
			if before, _, ok := strings.Cut(line, label); ok {
				column := ansi.StringWidth(before) + 1
				stable := x == column && y == row+1
				x, y = column, row+1
				return stable
			}
		}
		x, y = -1, -1
		return false
	})
	h.send(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y, x, y))
}

func TestPTYUICompactThemes(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || os.Getenv("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("explicit live opt-in required")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux PTY acceptance")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	testPTYUICompactThemesStruct(t, &binary)
}

func testLiveOllamaPTYUIMessages(messages []ptyUIMessage, toolAt *int, instructionAt *int, finalAt *int) {
	for i, msg := range messages {
		if msg.Role == "tool" && strings.Contains(msg.allContent(), "LIVE_INPUT:HUMAN_PTY_OK") {
			(*toolAt) = i
		}
		if msg.Role == "user" && strings.Contains(msg.text(), "LIVE_STEERING_OK") {
			(*instructionAt) = i
		}
		if msg.Role == "assistant" && msg.finished() && strings.Contains(msg.text(), "LIVE_STEERING_OK") {
			(*finalAt) = i
		}
	}
}

func testLiveOllamaPTYUIPtyUIMessagesConn(t *testing.T, h *ptyUIHarness, conn *sql.DB, approved *bool) {
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
				(*approved) = true
			} else {
				h.send("d")
				t.Fatal("unexpected shell command or non-PTY request was denied")
			}
		}
	}
}

func testLiveOllamaPTYUIStruct(h *ptyUIHarness) {
	for _, keys := range []struct{ next, previous string }{
		{"\x1b[1;3C", "\x1b[1;3D"},         // Xterm Alt+arrows.
		{"\x1b[57351;3u", "\x1b[57350;3u"}, // CSI-u Alt+arrows.
		{"\t", "\x1b[Z"},                   // Tab/Shift+Tab fallback.
	} {
		h.send(keys.next)
		h.wait("second provider and distinct catalog", 10*time.Second, func(frame string) bool {
			return strings.Contains(frame, "Navigation fixture") && strings.Contains(frame, "UI_NAVIGATION_ONLY_MODEL") && strings.Contains(frame, "Context: 128k")
		})
		h.send(keys.previous)
		h.wait("Ollama provider restored", 10*time.Second, func(frame string) bool {
			return strings.Contains(frame, "Choose model · Ollama") && strings.Contains(frame, "Provider catalog") && !strings.Contains(frame, "UI_NAVIGATION_ONLY_MODEL")
		})
	}
}

func testLiveOllamaPTYUIPtyUIMessagesConnCase(conn *sql.DB, bashCalls *int) {
	for _, msg := range ptyUIMessages(conn) {
		for _, part := range msg.Parts {
			var call struct{ Name string }
			if part.Type == "tool_call" && json.Unmarshal(part.Data, &call) == nil && call.Name == "bash" {
				(*bashCalls)++
			}
		}
	}
}

func testLiveOllamaPTYUIPtyUIMessagesConnBeforeQuestion(t *testing.T, conn *sql.DB, beforeQuestion int, questions *int) {
	for _, msg := range ptyUIMessages(conn)[beforeQuestion:] {
		for _, part := range msg.Parts {
			if part.Type != "tool_call" {
				continue
			}
			var call struct{ Name string }
			require.NoError(t, json.Unmarshal(part.Data, &call))
			if call.Name == "ask_user_question" {
				(*questions)++
			}
		}
	}
}

func testLiveOllamaPTYUIOPCODELIVEOLLAMA(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || os.Getenv("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("requires explicit OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 opt-in")
	}
}

func testLiveOllamaPTYUIState(t *testing.T, workspace string, h *ptyUIHarness) {
	for _, refuse := range []string{"\x1b", "\r"} {
		require.Contains(t, h.frame(), "› Deny (Esc/d)")
		h.send(refuse)
		h.wait("approval dialog dismissed", 5*time.Second, func(frame string) bool {
			return !strings.Contains(frame, "Allow (a)") && strings.Contains(frame, "Test failed:")
		})
		_, statErr := os.Stat(filepath.Join(workspace, ".cache/ui-acceptance-artifacts", "lsp-stderr-ran"))
		require.True(t, os.IsNotExist(statErr), "denied probe must never run")
		h.send("\x14")
		h.wait("explicit permission dialog", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "› Deny (Esc/d)") })
	}
}

func testPTYUICompactThemesStruct(t *testing.T, binary *string) {
	for _, variant := range []struct {
		name, bg string
		noColor  bool
	}{{"dark", "15;0", false}, {"light", "0;15", false}, {"no-color", "0;15", true}} {
		t.Run(variant.name, func(t *testing.T) { testPTYUICompactThemesStructCallback(&binary, &variant, t) })
	}
}

func writeFPending(f *ptyUIControlFilter, controls []string, output *[]byte) {
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
		// vt10x does not retain split UTF-8 runes between Write calls. A
		// partial three/four-byte rune can even consume its leading byte,
		// so honoring its returned byte count alone cannot recover it.
		if !utf8.FullRune(f.pending) {
			break
		}
		_, size := utf8.DecodeRune(f.pending)
		(*output) = append((*output), f.pending[:size]...)
		f.pending = f.pending[size:]
	}
}

func testLiveOllamaPTYUIFactualCompletedOverview(conn **sql.DB, sourceCitation **regexp.Regexp, frame string) bool {
	if !ptyUIIdleFooter(frame) {
		return false
	}
	messages := ptyUIMessages((*conn))
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	text := strings.ToLower(last.text())
	return last.Role == "assistant" && last.finished() && strings.Contains(text, "igarapewatch") && strings.Contains(text, "sqlite") && strings.Contains(text, "sensor") && (*sourceCitation).MatchString(text)
}

func testLiveOllamaPTYUIHumanQuestionAnswerResumesGeneration(conn **sql.DB, beforeQuestion *int, frame string) bool {
	history := ptyUIMessages((*conn))
	return len(history) > (*beforeQuestion) && history[len(history)-1].finished() && strings.Contains(history[len(history)-1].text(), "Resposta recebida:") && !strings.Contains(frame, "Waiting for your answer") && ptyUIIdleFooter(frame)
}

func testLiveOllamaPTYUILSPFormReady(frame string) bool {
	// A VT frame arrives in fragments; the title alone can precede its fields.
	return strings.Contains(frame, "Configure LSP") && strings.Contains(frame, "00-ui-acceptance") && strings.Contains(frame, "/bin/bash")
}

func TestPTYUILSPFormWaitsForAllFields(t *testing.T) {
	require.False(t, testLiveOllamaPTYUILSPFormReady("Configure LSP"))
	require.False(t, testLiveOllamaPTYUILSPFormReady("Configure LSP\n00-ui-acceptance"))
	require.True(t, testLiveOllamaPTYUILSPFormReady("Configure LSP\n00-ui-acceptance\n/bin/bash"))
}

func testLiveOllamaPTYUIFailedProviderStaysSelectedWithEmptyList(frame string) bool {
	return strings.Contains(frame, "Navigation fixture") && strings.Contains(frame, "Provider query failed") && strings.Contains(frame, "Empty list: query failed") && !strings.Contains(frame, "UI_NAVIGATION_ONLY_MODEL")
}

func testLiveOllamaPTYUICallback(catalogAvailable *atomic.Bool, w http.ResponseWriter, r *http.Request) {
	if !catalogAvailable.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if r.URL.Path != "/v1/models" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"models":[{"id":"UI_NAVIGATION_ONLY_MODEL","openai":{"max_model_len":131072}}]}`)
}

func testLiveOllamaPTYUIContextAndMeasuredProviderSpeedInFooter(frame string) bool {
	footer := ptyUIFooter(frame)
	return strings.Contains(footer, "/16k ctx") && strings.Contains(footer, "tok/s") && !strings.Contains(footer, "— tok/s")
}

func testLiveOllamaPTYUIRealModelQuestionWithSelectableChoices(frame string) bool {
	return strings.Contains(frame, "Question for you") && strings.Contains(frame, "Waiting for your answer") && strings.Contains(frame, "UI") && strings.Contains(frame, "Contexto")
}

func testPTYUICompactThemesStructCallback(binary **string, variant *struct {
	name    string
	bg      string
	noColor bool
}, t *testing.T) {
	root := t.TempDir()
	home, workspace := filepath.Join(root, "home"), filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".opcode"), 0700))
	require.NoError(t, os.MkdirAll(workspace, 0700))
	configuration := map[string]any{
		"data": map[string]any{"directory": filepath.Join(root, "data")}, "tui": map[string]any{"language": "en", "theme": "opcode", "sidebar": "hidden"},
		"runtime": map[string]any{"mode": "ask"}, "providers": map[string]any{"local": map[string]any{"baseURL": "http://127.0.0.1:11434/v1", "api": "ollama"}},
		"models": map[string]any{"local.llama3.1:8b": map[string]any{"provider": "local", "api_model": "llama3.1:8b", "api": "ollama", "context_window": 16384, "default_max_tokens": 2048}},
		"agents": map[string]any{"coder": map[string]any{"model": "local.llama3.1:8b"}},
	}
	raw, err := json.Marshal(configuration)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".opcode", "config.json"), raw, 0600))
	env := []string{"COLORFGBG=" + variant.bg}
	if variant.noColor {
		env = append(env, "NO_COLOR=1")
	}
	h := newPTYUIHarnessAtSize(t, (*(*binary)), home, workspace, 60, 20, env...)
	h.wait("compact initialization", 20*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Initialize Project") && strings.Contains(frame, "Create Opcode.md") && strings.Contains(frame, "Not now")
	})
	h.send("n")
	h.wait("compact input", 10*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.assertComposer()
	h.paste("compact draft ação")
	h.wait("compact draft typed", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "compact draft ação") })
	h.send("\x0f")
	h.wait("compact model selector", 10*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Choose model") && strings.Contains(frame, "Context in use: 16k") && strings.Contains(frame, "Tools:")
	})
	require.Equal(t, 1, strings.Count(h.frame(), "Context in use:"), "equal context limits must use one compact row")
	require.Contains(t, h.frame(), "›")
	h.send("\x1b")
	h.wait("draft restored", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Choose model") && strings.Contains(frame, "compact draft ação")
	})
	h.send("\x06")
	h.wait("compact file picker", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Attach file") && strings.Contains(frame, "Empty directory")
	})
	h.send("\rl")
	h.wait("empty picker stays usable", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Attach file") })
	h.send("\x1b")
	h.wait("file picker draft restored", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Attach file") && strings.Contains(frame, "compact draft ação")
	})
	h.send("\x03")
	h.wait("quit menu", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Continue") && strings.Contains(frame, "Quit") })
	h.clickLabel("Continue")
	h.wait("quit cancelled by mouse", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Continue") && strings.Contains(frame, "compact draft ação")
	})
	h.send("\x14")
	h.wait("theme after quit click", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Select Theme") && strings.Contains(frame, "›")
	})
	h.send("\x05")
	h.wait("compact theme editor", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Edit theme") })
	h.send("\t")
	h.resize(80, 24)
	h.wait("theme editor survives resize", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Edit theme") })
	h.send("\x1b")
	h.wait("theme preview cancelled", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Select Theme") && !strings.Contains(frame, "Edit theme")
	})

	h.send("\x1b")
	h.wait("theme dismissed", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Select Theme") && strings.Contains(frame, "compact draft ação")
	})
	h.resize(80, 24)
	h.wait("resized draft intact", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "compact draft ação") })
	if variant.noColor {
		require.NotRegexp(t, regexp.MustCompile("\x1b\\[(?:38|48);"), h.rawOutput(), "NO_COLOR must suppress foreground/background color sequences")
	}
	require.NoFileExists(t, filepath.Join(workspace, "Opcode.md"), "declining init must not create project memory")
	require.NoError(t, os.WriteFile("/tmp/opcode-ui-"+variant.name+"-frame.txt", []byte(h.frame()), 0600))
}

func testLiveOllamaPTYUISteeredResponseAndPersistedToolResult(conn **sql.DB, frame string) bool {
	if !ptyUIIdleFooter(frame) {
		return false
	}
	messages := ptyUIMessages((*conn))
	toolAt, instructionAt, finalAt := -1, -1, -1
	testLiveOllamaPTYUIMessages(messages, &toolAt, &instructionAt, &finalAt)
	return toolAt >= 0 && instructionAt > toolAt && finalAt > instructionAt
}

func testLiveOllamaPTYUIInitializationDismissed(frame string) bool {
	return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
}

func testLiveOllamaPTYUIEightDraftRows(frame string) bool {
	return strings.Contains(frame, "draft-01 ação 漢字") && strings.Contains(frame, "draft-08")
}

func testLiveOllamaPTYUIProviderRetryRecoversCatalog(frame string) bool {
	return strings.Contains(frame, "UI_NAVIGATION_ONLY_MODEL") && !strings.Contains(frame, "query failed")
}

func testLiveOllamaPTYUIReturnToOllamaAfterOutage(frame string) bool {
	return strings.Contains(frame, "Choose model · Ollama") && strings.Contains(frame, "Provider catalog")
}

func testLiveOllamaPTYUIModelListPosition(frame string) bool {
	return strings.Contains(frame, " of ") && strings.Contains(frame, "In use") && !strings.Contains(frame, "✓ current")
}

func testLiveOllamaPTYUILocalModelSearch(frame string) bool {
	return strings.Contains(frame, "Search: llama3.1") && !strings.Contains(frame, "No models found")
}

func testLiveOllamaPTYUIModelSelectorDismissed(frame string) bool {
	return !strings.Contains(frame, "Choose model") && strings.Contains(frame, "draft-08")
}

func testLiveOllamaPTYUIConnectionListBeforeDeletion(frame string) bool {
	return strings.Contains(frame, "Connections and preferences") && strings.Contains(frame, "Navigation fixture")
}

func testLiveOllamaPTYUIDeleteConnectionConfirmation(frame string) bool {
	return strings.Contains(frame, "Delete connection?") && strings.Contains(frame, "Navigation fixture")
}

func testLiveOllamaPTYUICancelPreservesConnection(frame string) bool {
	return strings.Contains(frame, "Connections and preferences") && strings.Contains(frame, "Navigation fixture") && !strings.Contains(frame, "Delete connection?")
}

func testLiveOllamaPTYUIDeletedConnectionDisappears(frame string) bool {
	return strings.Contains(frame, "Connections and preferences") && !strings.Contains(frame, "Navigation fixture")
}

func testLiveOllamaPTYUIConnectionListDismissed(frame string) bool {
	return !strings.Contains(frame, "Connections and preferences") && ptyUIHasFooter(frame)
}

func testLiveOllamaPTYUIModeChooser(frame string) bool {
	return strings.Contains(frame, "Work mode") || strings.Contains(frame, "Modo de trabalho")
}

func testLiveOllamaPTYUIModeChooserDismissed(frame string) bool {
	return !strings.Contains(frame, "Work mode") && !strings.Contains(frame, "Modo de trabalho")
}

func testLiveOllamaPTYUITaskOverview(frame string) bool {
	return strings.Contains(frame, "Task") && strings.Contains(frame, "Expected outcome")
}

func testLiveOllamaPTYUIOverviewDismissed(frame string) bool {
	return !strings.Contains(frame, "Expected outcome") && ptyUIHasFooter(frame)
}

func testLiveOllamaPTYUIUnifiedContextPanel(frame string) bool {
	return strings.Contains(frame, "1 Budget") && strings.Contains(frame, "Source:")
}

func testLiveOllamaPTYUIContextDismissed(frame string) bool {
	return !strings.Contains(frame, "1 Budget") && ptyUIHasFooter(frame)
}

func testLiveOllamaPTYUILSPFormRestored(frame string) bool {
	return strings.Contains(frame, "Configure LSP") && !strings.Contains(frame, "one per row")
}

func testLiveOllamaPTYUIIntegrationDialogDismissed(frame string) bool {
	return !strings.Contains(frame, "MCP and language servers") && ptyUIHasFooter(frame)
}

func testLiveOllamaPTYUIOSC52EmittedWhileAlternateScreenIsActive(h **ptyUIHarness, expectedOSC *string, frame string) bool {
	return strings.Contains((*h).rawOutput(), (*expectedOSC)) && strings.Contains(frame, "receipt unconfirmed")
}

func testLiveOllamaPTYUISSHCopyPanelDismissed(frame string) bool {
	return !strings.Contains(frame, "Copy response") && ptyUIIdleFooter(frame)
}

func testLiveOllamaPTYUIExecuteModeAndAcknowledgedCommandSubmission(frame string) bool {
	// The workflow mode update precedes the editor submission receipt. Do
	// not append the next prompt to a draft still awaiting that receipt.
	return strings.HasPrefix(strings.TrimSpace(ptyUIFooter(frame)), "Execute") && !strings.Contains(frame, "/mode execute")
}

func TestPTYUITerminalStartupQueries(t *testing.T) {
	input := []byte("\x1b]11;?\x1b\\\x1b[6n")
	for split := 0; split <= len(input); split++ {
		var response bytes.Buffer
		screen := vt10x.New(vt10x.WithSize(80, 24), vt10x.WithWriter(&response))
		screen.Resize(60, 20)
		filter := ptyUIControlFilter{}
		_, err := screen.Write(filter.write(input[:split]))
		require.NoError(t, err)
		_, err = screen.Write(filter.write(input[split:]))
		require.NoError(t, err)
		require.Contains(t, response.String(), "\x1b]11;rgb:", "split %d", split)
		require.Contains(t, response.String(), "\x1b[1;1R", "split %d", split)
	}
}
