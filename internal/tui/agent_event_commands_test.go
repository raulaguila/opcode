package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/stretchr/testify/require"
)

type agentEventCommandProbe struct{ executed []string }

func (*agentEventCommandProbe) Init() tea.Cmd { return nil }
func (*agentEventCommandProbe) View() string  { return "" }
func (p *agentEventCommandProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var marker string
	switch msg.(type) {
	case page.ActivityMsg:
		marker = "activity"
	case page.AgentOutcomeMsg:
		marker = "outcome"
	default:
		return p, nil
	}
	return p, func() tea.Msg { p.executed = append(p.executed, marker); return nil }
}

func runCommandBatch(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, child := range batch {
			runCommandBatch(child)
		}
	}
}

func TestAgentEventsDeliverActivityOutcomeAndNotificationCommands(t *testing.T) {
	cfg, _ := emptySetupConfig(t)
	cfg.TUI.Notifications = "bell"
	before := deliverNotification
	t.Cleanup(func() { deliverNotification = before })
	for _, scenario := range []struct {
		name string
		done bool
		err  error
		want []string
	}{
		{"progress", false, nil, []string{"activity"}},
		{"complete", true, nil, []string{"activity", "notification", "outcome"}},
		{"failed", false, errors.New("fixture failure"), []string{"activity", "outcome"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			probe := &agentEventCommandProbe{}
			deliverNotification = func(string, bool, string) { probe.executed = append(probe.executed, "notification") }
			model := appModel{unfocused: true, currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: probe}}
			event := pubsub.Event[agent.AgentEvent]{Payload: agent.AgentEvent{Type: agent.AgentEventTypeResponse, Done: scenario.done, Error: scenario.err, Message: message.Message{SessionID: "fixture"}}}
			_, cmd := model.Update(event)
			runCommandBatch(cmd)
			require.Equal(t, scenario.want, probe.executed)
		})
	}
}
