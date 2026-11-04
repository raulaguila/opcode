package ui

import (
	"context"
	"errors"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type clearableSessions struct {
	session.Service
	list []session.Session
}

func (s *clearableSessions) List(context.Context) ([]session.Session, error) { return s.list, nil }
func (s *clearableSessions) Delete(_ context.Context, id string) error {
	s.list = slices.DeleteFunc(s.list, func(item session.Session) bool { return item.ID == id })
	return nil
}

func TestClearingConversationsTakesTwoPressesAndKeepsTheOpenOne(t *testing.T) {
	_, err := config.Load(t.TempDir(), false)
	require.NoError(t, err)
	sessions := &clearableSessions{list: []session.Session{{ID: "open", Title: "Aberta"}, {ID: "old", Title: "Antiga"}, {ID: "older", Title: "Mais antiga"}}}
	m := &Model{app: &app.App{Sessions: sessions}, width: 120, height: 40, session: session.Session{ID: "open", Title: "Aberta"}, numbered: "open"}
	require.NotNil(t, m)
	m.openSessions()
	d := m.dialog.(*sessionsDialog)

	press := tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}
	d.keys(m, press)
	require.Len(t, sessions.list, 3, "the first press only asks")
	require.Contains(t, m.notice, "2")

	d.keys(m, tea.KeyPressMsg{Code: tea.KeyDown})
	d.keys(m, press)
	require.Len(t, sessions.list, 3, "another key cancelled the confirmation")

	d.keys(m, press)
	require.Equal(t, []string{"open"}, []string{sessions.list[0].ID})
	require.Len(t, sessions.list, 1)
	require.Empty(t, m.numbered, "the header counts again")
}

func TestHeaderTitleOpensConversations(t *testing.T) {
	_, err := config.Load(t.TempDir(), false)
	require.NoError(t, err)
	m := &Model{app: &app.App{Sessions: &clearableSessions{}}, width: 100, height: 30}
	m.click(90, 0)
	require.Nil(t, m.dialog, "no conversation, no target")
	m.session = session.Session{ID: "s", Title: "Execute os testes"}
	m.click(5, 0)
	require.Nil(t, m.dialog, "the left of the header is not the title")
	m.click(90, 0)
	require.IsType(t, &sessionsDialog{}, m.dialog)
}

type refusingAgent struct {
	agent.Service
	refuse error
}

func (a refusingAgent) SetMode(context.Context, string, workflow.Mode) error { return nil }
func (a refusingAgent) IsSessionBusy(string) bool                            { return false }
func (a refusingAgent) Submit(context.Context, string, string, ...message.Attachment) (<-chan agent.AgentEvent, bool, error) {
	return nil, false, a.refuse
}

func (s *clearableSessions) Create(_ context.Context, title string) (session.Session, error) {
	created := session.Session{ID: "new", Title: title}
	s.list = append(s.list, created)
	return created, nil
}

func TestConversationIsRegisteredOnlyWhenAMessageIsSent(t *testing.T) {
	cfg, err := config.Load(t.TempDir(), false)
	require.NoError(t, err)
	last := func() string {
		_, id, err := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory).Snapshot()
		require.NoError(t, err)
		return id
	}
	sessions := &clearableSessions{}
	coder := refusingAgent{refuse: errors.New("provider down")}
	m := New(&app.App{Sessions: sessions, CoderAgent: coder})

	m.composer.SetValue("oi")
	m.submit()
	require.Empty(t, sessions.list, "a message that never left leaves no conversation")
	require.Empty(t, m.session.ID)
	require.Empty(t, last())
	require.Equal(t, "oi", m.composer.Value(), "the message waits to be sent again")

	m.app.CoderAgent = refusingAgent{}
	m.submit()
	require.Equal(t, "new", last())
}

func TestApprovalLevelFollowsConversationsAndFullAccessDoesNot(t *testing.T) {
	_, err := config.Load(t.TempDir(), false)
	require.NoError(t, err)
	service := permission.NewPermissionService()
	policies := service.(permission.ApprovalPolicies)
	m := New(&app.App{Permissions: service, Messages: emptyMessages{}, Sessions: &clearableSessions{}}, session.Session{ID: "first"})
	require.Equal(t, permission.ApprovalSandboxed, policies.ApprovalPolicy("first"), "a resumed conversation gets the default level")

	m.setApproval(permission.ApprovalAuto)
	m.openSession(session.Session{ID: "second"})
	require.Equal(t, permission.ApprovalAuto, policies.ApprovalPolicy("second"), "the footer must not lie after a switch")

	m.width, m.height = 100, 30
	m.openApproval()
	d := m.dialog.(*listDialog)
	require.Equal(t, 1, d.list.Selected)
	require.Equal(t, "✓", d.list.Items[1].Mark)
	d.list.Selected = 2
	screen := ansi.Strip(d.view(m))
	require.Contains(t, screen, "│")
	require.NotContains(t, screen, "…", "the description must fit whole")

	d.choose(m, d.list.Items[2])
	require.Equal(t, permission.ApprovalAuto, policies.ApprovalPolicy("second"), "full access takes a second Enter")
	d.choose(m, d.list.Items[2])
	require.Equal(t, permission.ApprovalFull, policies.ApprovalPolicy("second"))

	m.openSession(session.Session{ID: "third"})
	require.Equal(t, permission.ApprovalSandboxed, policies.ApprovalPolicy("second"), "full access ends with its conversation")
	require.Equal(t, permission.ApprovalSandboxed, policies.ApprovalPolicy("third"))
}

type emptyMessages struct{ message.Service }

func (emptyMessages) List(context.Context, string) ([]message.Message, error) { return nil, nil }
