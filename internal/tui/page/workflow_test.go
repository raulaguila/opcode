package page

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type workflowProbe struct {
	agent.Service
	store    *workflow.Store
	busy     bool
	canceled int
}

func (p *workflowProbe) SetMode(ctx context.Context, id string, mode workflow.Mode) error {
	if p.busy {
		return agent.ErrSessionBusy
	}
	_, err := p.store.SetMode(ctx, id, mode)
	return err
}
func (p *workflowProbe) Cancel(string) { p.canceled++ }
func workflowPage(t *testing.T) *chatPage {
	t.Helper()
	store := workflow.New(nil, t.TempDir(), workflow.Execute)
	questions := interaction.NewQuestions()
	questions.SetInteractive(true)
	application := &app.App{Workflows: store, Questions: questions, CoderAgent: &workflowProbe{store: store}, Processes: tools.NewProcessManager()}
	t.Cleanup(func() { _ = application.Processes.Close() })
	p := NewChatPage(application).(*chatPage)
	p.session = session.Session{ID: "s"}
	p.SetSize(80, 24)
	p.refreshWorkflow()
	return p
}
func TestWorkflowCommandsChangeModeAndBlockHumanTerminal(t *testing.T) {
	p := workflowPage(t)
	cmd, handled := p.handleWorkflowMessage("/mode ask")
	require.True(t, handled)
	flattenCommands(cmd)
	require.Equal(t, workflow.Ask, p.mode())
	messages := flattenCommands(p.startTerminal())
	require.Len(t, messages, 1)
	info, ok := messages[0].(util.InfoMsg)
	require.True(t, ok)
	require.Contains(t, info.Msg, "execute mode")
	require.Empty(t, p.app.Processes.List("s"))
	p.app.CoderAgent.(*workflowProbe).busy = true
	flattenCommands(p.workflowCommand("mode", "execute"))
	require.Equal(t, workflow.Ask, p.mode())
}

func TestSlashEscapeClosesMenuBeforeCancelingAgent(t *testing.T) {
	p := workflowPage(t)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	require.True(t, p.slashMenuOpen())
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.False(t, p.slashMenuOpen())
	require.Zero(t, p.app.CoderAgent.(*workflowProbe).canceled)
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, 1, p.app.CoderAgent.(*workflowProbe).canceled)
}
func TestRejectedModeReportsFailureToEditor(t *testing.T) {
	p := workflowPage(t)
	p.app.CoderAgent.(*workflowProbe).busy = true
	cmd, handled := p.handleWorkflowMessage("/mode ask")
	require.True(t, handled)
	found := false
	for _, msg := range flattenCommands(cmd) {
		if ack, ok := msg.(chat.SubmissionResultMsg); ok {
			found = true
			require.Equal(t, "/mode ask", ack.Text)
			require.ErrorIs(t, ack.Error, agent.ErrSessionBusy)
		}
	}
	require.True(t, found)
	require.Equal(t, workflow.Execute, p.mode())
}

func TestResumeAcknowledgesOriginalDraftAndReportsQueueRejection(t *testing.T) {
	for _, reject := range []bool{false, true} {
		p := workflowPage(t)
		probe := &submittingAgent{queued: true}
		if reject {
			probe.err = errors.New("queue full")
		}
		p.app.CoderAgent = probe
		messages := flattenCommands(p.sendMessage("/resume", nil))
		require.Equal(t, []string{"s:" + resumeInstruction}, probe.seen)
		found := false
		for _, msg := range messages {
			if ack, ok := msg.(chat.SubmissionResultMsg); ok {
				found = true
				require.Equal(t, "/resume", ack.Text)
				require.Equal(t, probe.err, ack.Error)
			}
		}
		require.True(t, found)
	}
}
func TestQuestionUsesRealSelectionOrFreeTextWithoutSubmittingChat(t *testing.T) {
	for _, free := range []bool{false, true} {
		t.Run(map[bool]string{false: "choice", true: "free text"}[free], func(t *testing.T) {
			p := workflowPage(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan string, 1)
			go func() {
				answer, err := p.app.Questions.Ask(ctx, interaction.Question{SessionID: "s", Question: "Qual banco?", Choices: []string{"SQLite", "Postgres"}})
				if err != nil {
					done <- err.Error()
				} else {
					done <- answer
				}
			}()
			require.Eventually(t, func() bool { p.refreshWorkflow(); return p.workflow.question.ID != "" }, time.Second, time.Millisecond)
			flattenCommands(p.questionKey(tea.KeyMsg{Type: tea.KeyEnter}))
			select {
			case <-done:
				t.Fatal("Enter answered without an explicit choice")
			default:
			}
			want := "SQLite"
			if free {
				want = "Minha opção"
				p.questionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(want)})
			} else {
				p.questionKey(tea.KeyMsg{Type: tea.KeyDown})
			}
			flattenCommands(p.questionKey(tea.KeyMsg{Type: tea.KeyEnter}))
			require.Equal(t, want, <-done)
			require.Empty(t, p.workflow.question.ID)
		})
	}
}
func TestPlanRenderingShowsProgressAndRemainsBounded(t *testing.T) {
	p := workflowPage(t)
	_, err := p.app.Workflows.UpdatePlan(context.Background(), "s", "Corrigir erro", []workflow.Step{{Title: "Investigar", Status: "completed"}, {Title: "Validar", Status: "in_progress"}}, []string{"Teste passa"}, nil)
	require.NoError(t, err)
	p.workflow.expanded = true
	p.refreshWorkflow()
	view := p.workflowView()
	require.Contains(t, view, config.Text("Plano 1/2", "Plan 1/2"))
	require.Contains(t, view, "Validar")
	p.width, p.height = 24, 10
	require.LessOrEqual(t, p.workflowHeight(), p.height-5)
}
