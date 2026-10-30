//go:build integration

package tui

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// This is deliberately separate from provider smoke tests: it loads the real
// agent, all its tools, SQLite and the TUI, and executes an approved shell command.
// Run this test alone, since config.Load is a process-wide singleton.
func liveOllamaApplication(t *testing.T) (*app.App, context.Context) {
	t.Helper()
	if config.Environment("OPCODE_LIVE_TESTS") != "1" || config.Environment("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("requires OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 and -tags=integration")
	}
	if runtime.GOOS != "linux" {
		t.Skip("this live acceptance test requires Linux and Bubblewrap")
	}
	require.Nil(t, config.Get(), "run each live Ollama test in a separate go test process with -run")
	endpoint := os.Getenv("LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/v1"
	}
	u, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, u.Hostname(), "live acceptance test only permits a loopback Ollama endpoint")
	t.Setenv("LOCAL_ENDPOINT", endpoint)
	t.Setenv("OPCODE_DEV_DEBUG", "")
	model := config.Environment("OPCODE_LIVE_OLLAMA_MODEL")
	if model == "" {
		model = "llama3.1:8b"
	}
	localModel := "local." + model
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	require.NoError(t, os.Mkdir(workspace, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "interactive.sh"), []byte("[ -t 0 ] || exit 42\nprintf 'LIVE_READY\\n'\nIFS= read -r value\nprintf 'LIVE_INPUT:%s\\n' \"$value\"\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# IgarapeWatch\n\nAplicativo em Go para coletar métricas de sensores via MQTT, armazenar os dados em SQLite e gerar relatórios pela linha de comando.\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/igarapewatch\n\ngo 1.26\n"), 0600))
	configuration := map[string]any{
		"data":      map[string]any{"directory": filepath.Join(root, "data")},
		"security":  map[string]any{"sandbox": "bubblewrap"},
		"shell":     map[string]any{"path": "/bin/bash", "args": []string{"--noprofile", "--norc"}},
		"providers": map[string]any{"local": map[string]any{"baseURL": endpoint}},
		"models":    map[string]any{localModel: map[string]any{"provider": "local", "api_model": model, "api": "ollama", "context_window": 16384, "default_max_tokens": 2048}},
		"agents": map[string]any{
			"coder":      map[string]any{"model": localModel},
			"title":      map[string]any{"model": localModel},
			"summarizer": map[string]any{"model": localModel},
			"task":       map[string]any{"model": localModel},
		},
		"autoCompact": false,
	}
	raw, err := json.Marshal(configuration)
	require.NoError(t, err)
	path := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(path, raw, 0600))
	viper.SetConfigFile(path)
	cfg, err := config.Load(workspace, false)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateStartup())
	require.Equal(t, "bubblewrap", cfg.Security.Sandbox)
	conn, err := db.Connect()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	application, err := app.New(ctx, conn)
	require.NoError(t, err)
	t.Cleanup(application.Shutdown)
	return application, ctx
}

func TestLiveOllamaProjectOverview(t *testing.T) {
	application, ctx := liveOllamaApplication(t)
	sess, err := application.Sessions.Create(ctx, "Project overview")
	require.NoError(t, err)
	requests := application.Permissions.Subscribe(ctx)
	done, err := application.CoderAgent.Run(ctx, sess.ID, "o que pode me falar sobre o projeto atual?")
	require.NoError(t, err)
	var result agent.AgentEvent
	select {
	case result = <-done:
	case request := <-requests:
		application.Permissions.Deny(request.Payload)
		t.Fatal("overview unexpectedly requested a privileged operation; denied")
	case <-ctx.Done():
		t.Fatal("overview timed out")
	}
	require.NoError(t, result.Error)
	history, err := application.Messages.List(ctx, sess.ID)
	require.NoError(t, err)
	readProject := false
	for _, msg := range history {
		for _, toolResult := range msg.ToolResults() {
			if !toolResult.IsError && strings.Contains(toolResult.Content, "IgarapeWatch") {
				readProject = true
			}
		}
	}
	require.True(t, readProject, "model must read real project content before describing it")
	text := strings.ToLower(result.Message.Content().String())
	for _, fact := range []string{"igarapewatch", "sensor", "sqlite"} {
		require.True(t, strings.Contains(text, fact), "overview missed a fact from README; response suppressed")
	}
	require.NotContains(t, text, "import subprocess")
	t.Log("natural Portuguese question led to real file inspection and a factual project overview")
}

func TestLiveOllamaTerminalAndSteering(t *testing.T) {
	application, ctx := liveOllamaApplication(t)
	harness := &terminalUIHarness{model: New(application), frames: make(chan string, 256)}
	program := tea.NewProgram(harness, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	requests := application.Permissions.Subscribe(ctx)
	completed := application.CoderAgent.Subscribe(ctx)
	liveForward(ctx, program, application.Messages.Subscribe(ctx))
	liveForward(ctx, program, application.Sessions.Subscribe(ctx))
	liveForward(ctx, program, application.CoderAgent.Subscribe(ctx))
	liveForward(ctx, program, application.Permissions.Subscribe(ctx))
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	defer program.Quit()
	diagnosticSession := ""
	lastFrame := ""
	waitFrame := func(marker string) {
		testLiveOllamaTerminalAndSteeringCallback(&t, &application, &ctx, &harness, &diagnosticSession, &lastFrame, marker)
	}
	sendText := func(text string) {
		program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
		program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	}
	waitFrame("Opcode")
	program.Send(tea.WindowSizeMsg{Width: 160, Height: 40})
	const command = "bash interactive.sh"
	arguments, err := json.Marshal(tools.BashParams{Command: command, PTY: true, Timeout: 120000})
	require.NoError(t, err)
	initial := `Call bash with these arguments: ` + string(arguments) + `. Wait for the actual tool result. The human will provide terminal input.`
	sendText(initial)
	request := awaitLiveCommandApproval(t, application, ctx, requests, completed)
	diagnosticSession = request.SessionID
	validateLiveCommandApproval(t, application, request, command)
	require.Empty(t, application.Processes.List(request.SessionID), "nothing executes before UI approval")
	waitFrame("Allow (a)")
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	waitFrame("LIVE_READY")
	processes := application.Processes.List(request.SessionID)
	require.Len(t, processes, 1)
	process := processes[0]
	require.True(t, process.PTY)
	require.True(t, process.Running, "partial output appears while read is still blocked")
	t.Log("real model called bash; UI approval and live output passed with Bubblewrap enabled")

	const instruction = "Nova instrução: quando o comando atual terminar, responda apenas LIVE_STEERING_OK. Não execute mais ferramentas."
	require.True(t, application.CoderAgent.IsSessionBusy(request.SessionID))
	sendText(instruction)
	select {
	case event := <-completed:
		require.Equal(t, agent.AgentEventTypeInstructions, event.Payload.Type)
		require.Equal(t, request.SessionID, event.Payload.SessionID)
	case <-ctx.Done():
		t.Fatal("new instruction was not queued")
	}
	waitFrame("queued")
	snapshot, err := application.Processes.Snapshot(request.SessionID, process.ID)
	require.NoError(t, err)
	require.True(t, snapshot.Running, "new instruction accepted before the blocked command completes")
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	waitFrame("Terminal • running")
	sendText("HUMAN_PTY_OK")
	waitFrame("LIVE_INPUT:HUMAN_PTY_OK")
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	t.Log("new instruction queued while agent was busy; human input reached its real PTY")
	for {
		select {
		case event := <-completed:
			if !event.Payload.Done && event.Payload.Error == nil {
				continue
			}
			require.NoError(t, event.Payload.Error)
			require.True(t, strings.Contains(event.Payload.Message.Content().String(), "LIVE_STEERING_OK"), "model did not follow the queued instruction; response suppressed")
			goto finished
		case event := <-requests:
			application.Permissions.Deny(event.Payload)
			t.Fatalf("model requested an additional %s operation after the new instruction; denied", event.Payload.ToolName)
		case <-ctx.Done():
			t.Fatal("agent did not complete after human input")
		}
	}
finished:
	history, err := application.Messages.List(ctx, request.SessionID)
	require.NoError(t, err)
	toolIndex, instructionIndex := -1, -1
	for i, msg := range history {
		testLiveOllamaTerminalAndSteeringRoleMessageTool(t, &toolIndex, i, &msg)
		if msg.Role == message.User && msg.Content().String() == instruction {
			instructionIndex = i
		}
	}
	require.GreaterOrEqual(t, toolIndex, 0)
	require.Greater(t, instructionIndex, toolIndex, "tool result persists before queued instruction")
	snapshot, err = application.Processes.Snapshot(request.SessionID, process.ID)
	require.NoError(t, err)
	require.False(t, snapshot.Running)
	require.Zero(t, snapshot.ExitCode)
	require.False(t, snapshot.TimedOut)
	waitFrame("LIVE_STEERING_OK")
	program.Quit()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("UI failed to shut down")
	}
	t.Log("real model followed the new instruction; persisted tool order, exit status and UI completion passed")
}

func liveForward[T any](ctx context.Context, program *tea.Program, events <-chan pubsub.Event[T]) {
	go func() {
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				program.Send(event)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func testLiveOllamaTerminalAndSteeringState(t *testing.T, application *app.App, ctx context.Context, harness *terminalUIHarness, diagnosticSession string, lastFrame *string, marker string, deadline *time.Timer) (handled bool) {
	for {
		select {
		case frame := <-harness.frames:
			(*lastFrame) = frame
			if strings.Contains(frame, marker) {
				return true
			}
		case <-deadline.C:
			testLiveOllamaTerminalAndSteeringStateDiagnosticSession(t, application, ctx, diagnosticSession)
			if len((*lastFrame)) > 2000 {
				(*lastFrame) = (*lastFrame)[len((*lastFrame))-2000:]
			}
			t.Logf("Last UI frame: %q", tools.RedactCredentials((*lastFrame)))
			t.Fatalf("UI did not show %q", marker)
		case <-ctx.Done():
			t.Fatal("live test deadline exceeded")
		}
	}
}

func testLiveOllamaTerminalAndSteeringStateDiagnosticSession(t *testing.T, application *app.App, ctx context.Context, diagnosticSession string) {
	if diagnosticSession != "" {
		for _, process := range application.Processes.List(diagnosticSession) {
			snapshot, e := application.Processes.Snapshot(diagnosticSession, process.ID)
			if e == nil {
				t.Logf("Process diagnostic: pty=%t running=%t exit=%d timed_out=%t stdout=%q stderr=%q", snapshot.PTY, snapshot.Running, snapshot.ExitCode, snapshot.TimedOut, tools.RedactCredentials(snapshot.Stdout), tools.RedactCredentials(snapshot.Stderr))
			}
		}
		testLiveOllamaTerminalAndSteeringStateDiagnosticSessionListCtxDiagnosticSession(t, application, ctx, diagnosticSession)
	}
}

func testLiveOllamaTerminalAndSteeringRoleMessageTool(t *testing.T, toolIndex *int, i int, msg *message.Message) {
	if msg.Role == message.Tool {
		for _, result := range msg.ToolResults() {
			if strings.Contains(result.Content, "LIVE_INPUT:HUMAN_PTY_OK") {
				require.False(t, result.IsError)
				(*toolIndex) = i
			}
		}
	}
}

func testLiveOllamaTerminalAndSteeringStateDiagnosticSessionListCtxDiagnosticSession(t *testing.T, application *app.App, ctx context.Context, diagnosticSession string) {
	if history, e := application.Messages.List(ctx, diagnosticSession); e == nil {
		for _, msg := range history {
			for _, result := range msg.ToolResults() {
				text := tools.RedactCredentials(result.Content)
				if len(text) > 400 {
					text = text[:400]
				}
				t.Logf("Tool diagnostic: name=%s error=%t result=%q", result.Name, result.IsError, text)
			}
		}
	}
}

func testLiveOllamaTerminalAndSteeringCallback(t **testing.T, application **app.App, ctx *context.Context, harness **terminalUIHarness, diagnosticSession *string, lastFrame *string, marker string) {
	(*t).Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	if handled := testLiveOllamaTerminalAndSteeringState((*t), (*application), (*ctx), (*harness), (*diagnosticSession), lastFrame, marker, deadline); handled {
		return
	}
}

func awaitLiveCommandApproval(t *testing.T, application *app.App, ctx context.Context, requests <-chan pubsub.Event[permission.PermissionRequest], completed <-chan pubsub.Event[agent.AgentEvent]) permission.PermissionRequest {
	t.Helper()
	var request permission.PermissionRequest
	select {
	case event := <-requests:
		request = event.Payload
	case event := <-completed:
		if session, err := application.Sessions.Get(ctx, event.Payload.Message.SessionID); err == nil {
			t.Logf("agent usage: input=%d output=%d", session.PromptTokens, session.CompletionTokens)
		}
		t.Fatalf("agent completed without requesting command approval (event=%s, error=%t)", event.Payload.Type, event.Payload.Error != nil)
	case <-ctx.Done():
		t.Fatal("model never requested command approval")
	}
	return request
}

func validateLiveCommandApproval(t *testing.T, application *app.App, request permission.PermissionRequest, command string) {
	t.Helper()
	params, ok := request.Params.(tools.BashPermissionsParams)
	if !ok || request.ToolName != tools.BashToolName || params.Command != command || !params.PTY {
		application.Permissions.Deny(request)
		t.Fatalf("model requested an unexpected operation; denied (tool=%s, bash_params=%t, command=%q, pty=%t)", request.ToolName, ok, params.Command, params.PTY)
	}
}
