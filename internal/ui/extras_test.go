package ui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/extensions"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type queueAgent struct {
	agent.Service
	queued  []agent.QueuedInstruction
	skilled []string
}

func (a *queueAgent) QueuedInstructions(string) []agent.QueuedInstruction { return a.queued }
func (a *queueAgent) TakeInstruction(_, id string) (agent.QueuedInstruction, error) {
	for i, q := range a.queued {
		if q.ID == id {
			a.queued = append(a.queued[:i], a.queued[i+1:]...)
			return q, nil
		}
	}
	return agent.QueuedInstruction{}, nil
}
func (a *queueAgent) Skills(context.Context) ([]extensions.Skill, error) { return nil, nil }
func (a *queueAgent) InvokeSkill(_ context.Context, _, name, args string) (<-chan agent.AgentEvent, bool, error) {
	a.skilled = append(a.skilled, name, args)
	return nil, false, nil
}
func (a *queueAgent) Submit(context.Context, string, string, ...message.Attachment) (<-chan agent.AgentEvent, bool, error) {
	panic("a skill call must not go out as a plain message")
}

func TestQueueTakesAMessageBackIntoTheComposer(t *testing.T) {
	coder := &queueAgent{queued: []agent.QueuedInstruction{{ID: "1", Content: "primeira"}, {ID: "2", Content: "segunda"}}}
	m := New(&app.App{CoderAgent: coder})
	m.width, m.height = 100, 30
	m.openQueue()
	m.dialog.(*listDialog).key(m, tea.KeyPressMsg{Code: tea.KeyDelete})
	require.Len(t, coder.queued, 1, "Delete drops the selected message")
	m.dialog.(*listDialog).choose(m, Item{ID: "2", Title: "segunda"})
	require.Equal(t, "segunda", m.composer.Value())
	require.Empty(t, coder.queued)
}

func TestSkillCallGoesToTheSkill(t *testing.T) {
	coder := &queueAgent{}
	m := New(&app.App{CoderAgent: coder})
	m.session.ID = "s"
	handled, err := m.invokeSkill(context.Background(), "/skill revisar  o diff ")
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, []string{"revisar", "o diff"}, coder.skilled)
}

func TestChecksListPassAndFail(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	m.task = workflow.Task{Validation: []workflow.Receipt{{Name: "test", Passed: true}, {Name: "lint"}}}
	m.openChecks()
	view := ansi.Strip(m.dialog.(*listDialog).view(m))
	require.Contains(t, view, "test")
	require.Contains(t, view, "lint")
	require.Contains(t, view, "failed")
}
