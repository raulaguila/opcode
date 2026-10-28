package app

import (
	"context"
	"errors"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
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
