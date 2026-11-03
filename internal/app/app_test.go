package app

import (
	"context"
	"errors"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

type promptSessions struct{ session.Service }

func (promptSessions) Create(context.Context, string) (session.Session, error) {
	return session.Session{ID: "non-interactive"}, nil
}

type permissionProbeAgent struct {
	agent.Service
	permissions permission.Service
	directory   string
	allowed     bool
	stop        error
}

func (a *permissionProbeAgent) Run(ctx context.Context, sessionID, _ string, _ ...message.Attachment) (<-chan agent.AgentEvent, error) {
	a.allowed = a.permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID: sessionID, ToolName: "bash", Action: "execute", Path: a.directory,
	})
	return nil, a.stop
}

func TestNonInteractiveRequiresExplicitAutomaticApproval(t *testing.T) {
	for _, autoApprove := range []bool{false, true} {
		permissions := permission.NewPermissionService()
		probe := &permissionProbeAgent{permissions: permissions, directory: t.TempDir(), stop: errors.New("probe complete")}
		app := &App{Sessions: promptSessions{}, Permissions: permissions, CoderAgent: probe}
		err := app.RunNonInteractive(context.Background(), "test", "text", true, autoApprove)
		require.ErrorIs(t, err, probe.stop)
		require.Equal(t, autoApprove, probe.allowed)
	}
}

type costSessions struct {
	promptSessions
	events chan pubsub.Event[session.Session]
}

func (s costSessions) Subscribe(context.Context) <-chan pubsub.Event[session.Session] {
	return s.events
}

type costlyAgent struct {
	agent.Service
	sessions  chan pubsub.Event[session.Session]
	cancelled chan struct{}
}

func (a *costlyAgent) Run(_ context.Context, id, _ string, _ ...message.Attachment) (<-chan agent.AgentEvent, error) {
	done := make(chan agent.AgentEvent, 1)
	go func() {
		a.sessions <- pubsub.Event[session.Session]{Payload: session.Session{ID: id, Cost: 0.75}}
		<-a.cancelled
		close(a.sessions)
		done <- agent.AgentEvent{Error: agent.ErrRequestCancelled}
	}()
	return done, nil
}

func (a *costlyAgent) Cancel(string) { close(a.cancelled) }

func TestNonInteractiveStopsAtTheCostLimit(t *testing.T) {
	events := make(chan pubsub.Event[session.Session])
	coder := &costlyAgent{sessions: events, cancelled: make(chan struct{})}
	app := &App{Sessions: costSessions{events: events}, Permissions: permission.NewPermissionService(), CoderAgent: coder, MaxCost: 0.5}
	err := app.RunNonInteractive(context.Background(), "test", "text", true, false)
	var exit *ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 3, exit.Code)
	require.Contains(t, err.Error(), "cost limit of $0.50")
}
