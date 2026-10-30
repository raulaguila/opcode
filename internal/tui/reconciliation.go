package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (a *appModel) reconcilePermissions(fallback permission.PermissionRequest) tea.Cmd {
	request := fallback
	if source, ok := a.app.Permissions.(interface {
		PendingRequestsAll() []permission.PermissionRequest
	}); ok {
		pending := source.PendingRequestsAll()
		for _, item := range pending {
			if a.showPermissions && item.ID == a.activePermissionID {
				return nil
			}
		}
		if len(pending) == 0 {
			a.showPermissions = false
			a.activePermissionID = ""
			return nil
		}
		slices.SortFunc(pending, func(a, b permission.PermissionRequest) int { return strings.Compare(a.ID, b.ID) })
		request = pending[0]
	}
	if request.ID == "" {
		return nil
	}
	a.showPermissions = true
	a.activePermissionID = request.ID
	activity, activityCmd := a.pages[page.ChatPage].Update(page.ActivityMsg{SessionID: request.SessionID, State: config.Text("Aguardando aprovação", "Waiting for approval")})
	a.pages[page.ChatPage] = activity
	return tea.Batch(a.permissions.SetPermissions(request), activityCmd, a.notify())
}

type sessionsRecoveredMsg struct {
	selectedID string
	version    uint64
	sessions   []session.Session
	err        error
}

func (a *appModel) recoverSessions() tea.Cmd {
	if a.sessionRecoveryPending || a.app == nil || a.app.Sessions == nil {
		return nil
	}
	a.sessionRecoveryPending = true
	id, service, version := a.selectedSession.ID, a.app.Sessions, a.sessionEventVersion
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sessions, err := service.List(ctx)
		return sessionsRecoveredMsg{selectedID: id, version: version, sessions: sessions, err: err}
	}
}

func (a *appModel) applyRecoveredSessions(msg sessionsRecoveredMsg) tea.Cmd {
	a.sessionRecoveryPending = false
	if msg.err != nil {
		return util.ReportError(msg.err)
	}
	if msg.version != a.sessionEventVersion {
		return a.recoverSessions()
	}
	if a.sessionDialog != nil {
		a.sessionDialog.SetSessions(msg.sessions)
	}
	if a.selectedSession.ID != msg.selectedID {
		return a.recoverSessions()
	}
	if msg.selectedID == "" {
		return nil
	}
	for _, item := range msg.sessions {
		if item.ID == msg.selectedID {
			a.selectedSession = item
			if pane := a.pages[page.ChatPage]; pane != nil {
				return util.CmdHandler(pubsub.Event[session.Session]{Type: pubsub.UpdatedEvent, Payload: item})
			}
			return nil
		}
	}
	return util.CmdHandler(chat.SessionClearedMsg{})
}
