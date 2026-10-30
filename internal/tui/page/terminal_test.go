package page

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestTerminalKeyEncoding(t *testing.T) {
	for _, tc := range []struct {
		key  tea.KeyMsg
		want string
	}{
		{tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		{tea.KeyMsg{Type: tea.KeyCtrlD}, "\x04"},
		{tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{tea.KeyMsg{Type: tea.KeyTab}, "\t"},
		{tea.KeyMsg{Type: tea.KeyEsc}, "\x1b"},
		{tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		{tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ação")}, "ação"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}, "\x1bx"},
	} {
		require.Equal(t, tc.want, terminalInput(tc.key))
	}
}

type submittingAgent struct {
	agent.Service
	seen   []string
	queued bool
	err    error
}

func (a *submittingAgent) Submit(_ context.Context, owner, text string, _ ...message.Attachment) (<-chan agent.AgentEvent, bool, error) {
	a.seen = append(a.seen, owner+":"+text)
	return nil, a.queued, a.err
}

func flattenCommands(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, child := range batch {
			messages = append(messages, flattenCommands(child)...)
		}
		return messages
	}
	return []tea.Msg{msg}
}

func TestChatSubmitsInstructionsWhileAgentWorks(t *testing.T) {
	for _, reject := range []bool{false, true} {
		probe := &submittingAgent{queued: true}
		if reject {
			probe.err = errors.New("instruction queue full")
		}
		p := &chatPage{app: &app.App{CoderAgent: probe}, session: session.Session{ID: "session"}}
		started := time.Now().Add(-10 * time.Second)
		p.progress, p.activityStarted = "Running command", started
		messages := flattenCommands(p.sendMessage("new instructions", nil))
		require.Equal(t, "Running command", p.progress)
		require.Equal(t, started, p.activityStarted)
		require.Equal(t, []string{"session:new instructions"}, probe.seen)
		found := false
		for _, msg := range messages {
			if ack, ok := msg.(chat.SubmissionResultMsg); ok {
				found = true
				require.Equal(t, "new instructions", ack.Text)
				require.Equal(t, probe.err, ack.Error)
			}
		}
		require.True(t, found)
	}
}

func TestTerminalViewShowsOutputAndResetsBetweenSessions(t *testing.T) {
	p := &chatPage{width: 80, height: 24, terminal: terminalPanel{
		focused: true, selected: "process", snapshot: tools.ProcessSnapshot{
			ProcessInfo: tools.ProcessInfo{ID: "process", Running: true, PTY: true, Command: "interactive command"},
			Screen:      "prompt: waiting for input", Stdout: "partial output\n",
		},
	}}
	require.Contains(t, p.View(), "waiting for input")
	require.Contains(t, p.View(), "Ctrl+P chat")
	p.terminal.focused = false
	require.Contains(t, p.terminalView(false), "partial output")
	p.resetTerminal()
	require.Empty(t, p.terminal.snapshot.Stdout)
	require.Empty(t, p.terminal.selected)
}

func TestRunningTerminalPreviewDoesNotRequireFocus(t *testing.T) {
	p := &chatPage{width: 80, height: 24, terminal: terminalPanel{selected: "process", snapshot: tools.ProcessSnapshot{ProcessInfo: tools.ProcessInfo{ID: "process", PTY: true, Running: true}, Stdout: "LIVE_READY\n"}}}
	require.True(t, p.terminalVisible())
	require.False(t, p.TerminalFocused())
	require.Contains(t, p.terminalView(false), "LIVE_READY")
	p.terminal.snapshot.Running = false
	require.False(t, p.terminalVisible(), "finished commands must not leave a permanent preview")
	p.terminal.snapshot.Running = true
	p.height = 14
	require.False(t, p.terminalVisible(), "small windows retain the composer space")
	p.terminal.focused = true
	require.True(t, p.terminalVisible(), "explicit full-screen terminal remains available")
}

func TestTerminalSnapshotReusePreservesScrollAndRefreshesCursor(t *testing.T) {
	p := &chatPage{width: 80, height: 12}
	snapshot := tools.ProcessSnapshot{ProcessInfo: tools.ProcessInfo{ID: "p", Running: true}, Stdout: "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve"}
	p.installTerminalSnapshot(snapshot)
	p.terminal.logs.SetYOffset(1)
	before := p.terminal.logs.YOffset
	snapshot.CursorX = 7
	snapshot.CursorVisible = true
	p.installTerminalSnapshot(snapshot)
	require.Equal(t, before, p.terminal.logs.YOffset)
	require.Equal(t, 7, p.terminal.snapshot.CursorX)
	require.True(t, p.terminal.snapshot.CursorVisible)
	snapshot.Stdout += "\nnew line"
	p.installTerminalSnapshot(snapshot)
	require.Equal(t, before, p.terminal.logs.YOffset, "streaming must not interrupt a user reading older terminal output")
	require.Contains(t, p.terminal.snapshot.Stdout, "new line")
}
func TestIdleWorkflowPollingUsesEventsAndKeepsQuestionMemoryFresh(t *testing.T) {
	p := &chatPage{app: &app.App{Workflows: workflow.New(nil, t.TempDir(), workflow.Execute)}, session: session.Session{ID: "s"}}
	p.refreshWorkflow()
	before := p.workflow.lastRead
	_, err := p.app.Workflows.UpdatePlan(context.Background(), "s", "new plan", []workflow.Step{{Title: "Read", Status: "pending"}}, []string{"Reviewed"}, nil)
	require.NoError(t, err)
	p.pollWorkflow()
	require.Equal(t, before, p.workflow.lastRead, "idle ticks must not query task persistence")
	require.NotEqual(t, "new plan", p.workflow.task.Objective)
	p.refreshWorkflow()
	require.Equal(t, "new plan", p.workflow.task.Objective, "explicit workflow events load authoritative state")
	p.progress = "Working"
	p.workflow.lastRead = time.Now().Add(-2 * time.Second)
	p.pollWorkflow()
	require.WithinDuration(t, time.Now(), p.workflow.lastRead, time.Second)
}
