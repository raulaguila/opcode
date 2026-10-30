package tui

import (
	"context"
	"database/sql"
	"testing"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

type initialSessionProbe struct {
	session.Service
	saved session.Session
	err   error
	calls int
}

func (p *initialSessionProbe) Get(context.Context, string) (session.Session, error) {
	p.calls++
	return p.saved, p.err
}

func TestInitialSessionPreferenceRestoresOnlyEligibleProjectConversation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		saved   session.Session
		err     error
		initial []session.Session
		want    string
		calls   int
	}{
		{name: "valid", saved: session.Session{ID: "last"}, want: "last", calls: 1},
		{name: "child", saved: session.Session{ID: "last", ParentSessionID: "parent"}, calls: 1},
		{name: "archived", saved: session.Session{ID: "last"}},
		{name: "empty"},
		{name: "deleted", err: sql.ErrNoRows, calls: 1},
		{name: "disabled", saved: session.Session{ID: "last"}},
		{name: "explicit", saved: session.Session{ID: "last"}, initial: []session.Session{{ID: "chosen"}}, want: "chosen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := emptySetupConfig(t)
			cfg.Preferences.ResumeLast = tc.name != "disabled"
			catalog := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory)
			if tc.name != "empty" {
				require.NoError(t, catalog.SaveDraft("last", "draft"))
			}
			if tc.name == "archived" {
				require.NoError(t, catalog.Archive("last", true))
			}
			probe := &initialSessionProbe{saved: tc.saved, err: tc.err}
			model := appModel{}
			newInitial(&app.App{Sessions: probe}, tc.initial, &model)
			require.Equal(t, tc.want, model.selectedSession.ID)
			require.Equal(t, tc.calls, probe.calls)
		})
	}
}
