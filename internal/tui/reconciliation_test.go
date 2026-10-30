package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/stretchr/testify/require"
)

type pendingSnapshotSource interface {
	PendingRequestsAll() []permission.PermissionRequest
}

func TestPermissionOverflowReconcilesWithoutApprovingOrDroppingPending(t *testing.T) {
	emptySetupConfig(t)
	service := permission.NewPermissionService()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = service.Subscribe(ctx)
	done := make(chan bool, 70)
	for range 70 {
		go func() {
			done <- service.Request(ctx, permission.CreatePermissionRequest{SessionID: "overflow", ToolName: "bash", Action: "execute", Path: t.TempDir()})
		}()
	}
	source := service.(pendingSnapshotSource)
	require.Eventually(t, func() bool { return len(source.PendingRequestsAll()) == 70 }, time.Second, time.Millisecond)
	pane := &agentEventCommandProbe{}
	model := appModel{app: &app.App{Permissions: service}, pages: map[page.PageID]tea.Model{page.ChatPage: pane}, permissions: dialog.NewPermissionDialogCmp()}
	_, _, _ = model.routePermission(pubsub.Event[permission.PermissionRequest]{ReconcileRequired: true})
	require.True(t, model.showPermissions)
	require.NotEmpty(t, model.activePermissionID)
	first := model.activePermissionID
	_, _, _ = model.routePermission(dialog.PermissionResponseMsg{Permission: permission.PermissionRequest{ID: first}, Action: dialog.PermissionDeny})
	require.True(t, model.showPermissions)
	require.NotEqual(t, first, model.activePermissionID)
	require.Len(t, source.PendingRequestsAll(), 69)
	cancel()
	for range 70 {
		require.False(t, <-done, "overflow/recovery must never grant access")
	}
}

type recoveredSessions struct {
	session.Service
	values []session.Session
}

func (s *recoveredSessions) List(context.Context) ([]session.Session, error) { return s.values, nil }

func TestSessionRecoveryRefreshesCurrentSelectionAndRejectsStaleSelection(t *testing.T) {
	service := &recoveredSessions{values: []session.Session{{ID: "new", Title: "current"}}}
	model := appModel{app: &app.App{Sessions: service}, selectedSession: session.Session{ID: "old"}}
	command := model.recoverSessions()
	require.NotNil(t, command)
	require.Nil(t, model.recoverSessions(), "only one reconciliation may be in flight")
	result := command().(sessionsRecoveredMsg)
	model.selectedSession = session.Session{ID: "new"}
	again := model.applyRecoveredSessions(result)
	require.NotNil(t, again, "a changed selection needs its own snapshot")
	require.Equal(t, "new", model.selectedSession.ID)
	require.Nil(t, model.applyRecoveredSessions(again().(sessionsRecoveredMsg)))
	require.Equal(t, "current", model.selectedSession.Title)
}

func TestSessionRecoveryCannotRegressAnUpdateReceivedDuringQuery(t *testing.T) {
	service := &recoveredSessions{values: []session.Session{{ID: "same", Title: "old"}}}
	model := appModel{app: &app.App{Sessions: service}, selectedSession: session.Session{ID: "same"}}
	result := model.recoverSessions()().(sessionsRecoveredMsg)
	_, _, _ = model.routeSession(pubsub.Event[session.Session]{Type: pubsub.UpdatedEvent, Payload: session.Session{ID: "same", Title: "fresh"}})
	service.values = []session.Session{{ID: "same", Title: "fresh"}}
	retry := model.applyRecoveredSessions(result)
	require.NotNil(t, retry)
	require.Equal(t, "fresh", model.selectedSession.Title)
	require.Nil(t, model.applyRecoveredSessions(retry().(sessionsRecoveredMsg)))
	require.Equal(t, "fresh", model.selectedSession.Title)
}

func TestRecoveredSessionUsesNormalEventDelivery(t *testing.T) {
	pane := &agentEventCommandProbe{}
	model := appModel{selectedSession: session.Session{ID: "active"}, pages: map[page.PageID]tea.Model{page.ChatPage: pane}}
	command := model.applyRecoveredSessions(sessionsRecoveredMsg{selectedID: "active", sessions: []session.Session{{ID: "active", Title: "recovered"}}})
	require.NotNil(t, command)
	event, ok := command().(pubsub.Event[session.Session])
	require.True(t, ok)
	require.Equal(t, pubsub.UpdatedEvent, event.Type)
	require.Equal(t, "recovered", event.Payload.Title)
	require.False(t, event.ReconcileRequired, "a recovery notification must not start an endless recovery loop")
}
