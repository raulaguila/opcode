package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func commandMessage(t *testing.T, id, command string) message.Message {
	t.Helper()
	input, err := json.Marshal(map[string]string{"command": command})
	require.NoError(t, err)
	return message.Message{ID: "m-" + id, Role: message.Assistant, CreatedAt: time.Now().Unix() - 5, Parts: []message.ContentPart{message.ToolCall{ID: id, Name: "bash", Input: string(input), Finished: true}}}
}

func TestCommandTranscriptWrapsTheWholeCommandAndShowsActualExit(t *testing.T) {
	m := New(nil)
	command := "cd /home/user/a-very-long-directory/project && go test ./... -run 'TestSomethingImportant'\nprintf '%s\\n' \"a  b\""
	m.messages = []message.Message{commandMessage(t, "test", command)}
	entry := m.commandEntries()[0]
	require.Equal(t, command, entry.command)
	rows := commandRows(entry, 40, false)
	var full strings.Builder
	for _, row := range rows[1 : len(rows)-1] {
		plain := ansi.Strip(row)
		require.LessOrEqual(t, ansi.StringWidth(plain), 40)
		full.WriteString(strings.TrimPrefix(plain, "    "))
	}
	require.Equal(t, strings.ReplaceAll(command, "\n", ""), full.String(), "wrapping must not omit command arguments or spaces")
	m.messages = append(m.messages, message.Message{ID: "r", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "test", IsError: true, Content: "test failed", Metadata: `{"exit_code":7}`}}})
	view := ansi.Strip(m.renderTranscript(60))
	require.Contains(t, view, "exit 7")
	require.Contains(t, view, "test failed")
	require.Contains(t, view, "Ctrl+P")
	for _, token := range []string{"Important", `printf '%s\n'`, `"a  b"`} {
		require.Contains(t, view, token)
	}
}

func TestLiveCommandOutputRepaintsWithoutANewMessage(t *testing.T) {
	m := New(nil)
	m.width, m.height, m.busy = 80, 30, true
	m.session.ID = "s"
	m.messages = []message.Message{commandMessage(t, "c", "go test ./...")}
	m.processes = []tools.ProcessSnapshot{{ProcessInfo: tools.ProcessInfo{ID: "p", SessionID: "s", Command: "go test ./...", Running: true, StartedAt: time.Now()}, Stdout: "first output"}}
	m.layout()
	view := ansi.Strip(strings.Join(m.rows, "\n"))
	require.Contains(t, view, "first output")
	require.Equal(t, 1, strings.Count(view, "go test ./..."), "a matched process must not add a duplicate command")
	m.processes[0].Stdout = "first output\nnew output"
	m.processVersion++
	m.refresh()
	require.Contains(t, ansi.Strip(strings.Join(m.rows, "\n")), "new output")
	m.processes[0].Running = false
	m.processes[0].ExitCode = 9
	m.processVersion++
	m.refresh()
	require.Contains(t, ansi.Strip(strings.Join(m.rows, "\n")), "exit 9")
	entry := m.commandEntries()[0]
	require.Equal(t, "p", entry.processID)
	require.Equal(t, "failed", entry.state)
}

func TestCommandsDoNotGuessWhichDuplicateProcessProducedOutput(t *testing.T) {
	m := New(nil)
	m.messages = []message.Message{commandMessage(t, "a", "go test"), commandMessage(t, "b", "go test")}
	m.processes = []tools.ProcessSnapshot{{ProcessInfo: tools.ProcessInfo{ID: "p", Command: "go test", Running: true, StartedAt: time.Now()}, Stdout: "actual process"}}
	entries := m.commandEntries()
	require.Len(t, entries, 3)
	require.Empty(t, entries[0].output)
	require.Empty(t, entries[1].output)
	require.Equal(t, "actual process", entries[2].output)
}

func TestCommandDetailsCopyLiteralTextPreserveDraftAndApproval(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "test")
	t.Setenv("OPCODE_TEST_API_KEY", "never-print-this-secret")
	m := New(nil)
	m.width, m.height = 100, 30
	command := "printf '%s\\n' \"$(touch should-not-execute)\"\n# never-print-this-secret"
	m.messages = []message.Message{commandMessage(t, "c", command)}
	m.approval = &permission.PermissionRequest{ID: "pending"}
	m.composer.SetValue("meu rascunho")
	m.key(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	d, ok := m.dialog.(*commandDialog)
	require.True(t, ok)
	view := ansi.Strip(d.view(m))
	require.Contains(t, view, "$(touch should-not-execute)")
	require.NotContains(t, view, "never-print-this-secret")
	cmd := m.key(tea.KeyPressMsg{Code: 'c', Text: "c"})
	require.NotNil(t, cmd)
	copy := cmd().(chatCopyMsg)
	require.NoError(t, copy.err)
	payload := strings.TrimSuffix(strings.TrimPrefix(copy.sequence, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err)
	require.Equal(t, safeCommandText(command), string(decoded))
	require.NotNil(t, m.approval)
	require.Equal(t, "pending", m.approval.ID)
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.Nil(t, m.dialog)
	require.Equal(t, "meu rascunho", m.composer.Value())
	require.NotNil(t, m.approval)
}

func TestCommandDetailsScrollCaptureAndKeepTheScreenSize(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.messages = []message.Message{commandMessage(t, "c", "go test ./...")}
	m.messages = append(m.messages, message.Message{ID: "r", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c", Content: strings.Repeat("line\n", 100) + "LAST_OUTPUT", Metadata: `{"exit_code":0,"truncated":true}`}}})
	m.layout()
	m.openCommands()
	d := m.dialog.(*commandDialog)
	d.key(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	view := ansi.Strip(d.view(m))
	require.Contains(t, view, "LAST_OUTPUT")
	require.Contains(t, view, "Partial output")
	screen := strings.Split(ansi.Strip(m.View().Content), "\n")
	require.Len(t, screen, 24)
	for _, row := range screen {
		require.Equal(t, 80, ansi.StringWidth(row))
	}
}

func TestAnsweredQuestionAppearsFromStoredRecordsOnResume(t *testing.T) {
	m := New(nil)
	m.session.ID = "s"
	m.width, m.height = 90, 30
	m.messages = []message.Message{
		{ID: "u", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "configure o banco"}}},
		{ID: "ask", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "q", Name: "ask_user_question", Input: `{"question":"Qual banco?","choices":["Postgres","SQLite"]}`, Finished: true}}},
		{ID: "answer", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "q", Content: `{"user_response":"SQLite"}`}}},
	}
	m.layout()
	view := ansi.Strip(strings.Join(m.rows, "\n"))
	require.Contains(t, view, "Question: Qual banco?")
	require.Contains(t, view, "Your answer: SQLite")
	require.NotContains(t, view, "ask_user_question")
	result := m.messages[2]
	result.SessionID = "s"
	result.Parts = []message.ContentPart{message.ToolResult{ToolCallID: "q", IsError: true, Content: "cancelled"}}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: result})
	require.NotContains(t, ansi.Strip(strings.Join(m.rows, "\n")), "Your answer: SQLite")
	require.Len(t, m.messages, 3, "displaying a decision does not create a fabricated user turn")
}

func TestCommandTextStripsHostTerminalControlSequences(t *testing.T) {
	call := message.ToolCall{Name: "bash", Input: `{"command":"echo safe\u001b]52;c;YXR0YWNr\u0007\r\nsuffix"}`}
	text := commandText(call)
	require.Equal(t, "echo safe\nsuffix", text)
	require.NotContains(t, text, "\x1b")
}

func TestSavedProcessStartShowsItsCapturedTextAfterRestart(t *testing.T) {
	m := New(nil)
	m.messages = []message.Message{{ID: "a", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "c", Name: "process_start", Input: `{"command":"go test"}`}}}, {ID: "r", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c", Content: `{"id":"p","command":"go test","running":true,"stdout":"FIRST","stderr":"WARN"}`}}}}
	entries := m.commandEntries()
	require.Len(t, entries, 1)
	require.Equal(t, "FIRST\nstderr:\nWARN", entries[0].output)
	require.Equal(t, "p", entries[0].processID)
	require.Equal(t, "unknown", entries[0].state, "a saved snapshot cannot prove that a process survived a restart")
	m.processes = []tools.ProcessSnapshot{{ProcessInfo: tools.ProcessInfo{ID: "p", Running: true}, Stdout: "FIRST\nNEW_OUTPUT"}}
	entries = m.commandEntries()
	require.Equal(t, "running", entries[0].state)
	require.Contains(t, entries[0].output, "NEW_OUTPUT")
}

func TestCommandInspectorReadsLiveOutputAndStopsOnlyItsSessionProcess(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(settings)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	cfg, err := config.Load(root, false)
	require.NoError(t, err)
	useWorkspace(t, root)
	sandbox := cfg.Security.Sandbox
	cfg.Security.Sandbox = "none" // This fixture tests UI process control, not host sandbox support.
	t.Cleanup(func() { cfg.Security.Sandbox = sandbox })
	manager := tools.NewProcessManager()
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	perms := permission.NewPermissionService()
	perms.AutoApproveSession("s")
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "s")
	info, err := manager.Start(ctx, perms, tools.ProcessParams{Command: "printf FIRST; sleep 30", Timeout: 5000})
	require.NoError(t, err)
	m := New(nil)
	m.app = &app.App{Processes: manager}
	m.session.ID = "s"
	m.width, m.height = 100, 30
	require.Eventually(t, func() bool {
		m.pollProcesses()
		entries := m.commandEntries()
		return len(entries) == 1 && entries[0].state == "running" && strings.Contains(entries[0].output, "FIRST")
	}, 3*time.Second, 10*time.Millisecond)
	m.openCommands()
	require.Contains(t, ansi.Strip(m.dialog.view(m)), "FIRST")
	cmd := m.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	require.NotNil(t, cmd)
	m.Update(cmd())
	snapshot, err := manager.Snapshot("s", info.ID)
	require.NoError(t, err)
	require.False(t, snapshot.Running)
	require.False(t, m.runningProcesses())
	m.session.ID = "other"
	require.Empty(t, m.commandEntries(), "another conversation's process must not appear")
}
