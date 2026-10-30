package cmd

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestResumeFlagAcceptsLastAndExistingIDSyntax(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{{nil, ""}, {[]string{"--resume"}, "last"}, {[]string{"--resume", "saved"}, "saved"}, {[]string{"--resume=saved"}, "saved"}, {[]string{"--resume", "--prompt", "continue"}, "last"}, {[]string{"--resume", "saved", "--prompt", "continue"}, "saved"}} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			command := &cobra.Command{Use: "opcode"}
			command.AddCommand(&cobra.Command{Use: "doctor", Run: func(*cobra.Command, []string) { t.Fatal("unexpected subcommand") }})
			addResumeFlag(command)
			command.Flags().String("prompt", "", "")
			command.RunE = func(cmd *cobra.Command, args []string) error {
				got, err := resumeRequest(cmd, args)
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
				return nil
			}
			command.SetArgs(tc.args)
			require.NoError(t, command.Execute())
		})
	}
}

func TestResumeLastUsesOnlyCurrentProjectCatalogAndSkipsStaleSessions(t *testing.T) {
	failure := errors.New("read failed")
	for _, tc := range []struct {
		name               string
		result             session.Session
		lookupErr, wantErr error
		wantID             string
		lookups            int
	}{
		{name: "present", result: session.Session{ID: "saved"}, wantID: "saved", lookups: 1},
		{name: "absent"},
		{name: "archived"},
		{name: "deleted", lookupErr: sql.ErrNoRows, lookups: 1},
		{name: "child", result: session.Session{ID: "saved", ParentSessionID: "parent"}, lookups: 1},
		{name: "storage failure", lookupErr: failure, wantErr: failure, lookups: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := resumeLastFixture(t, tc.name)
			lookups := 0
			saved, err := resolveResumeSession(context.Background(), cfg, "last", func(_ context.Context, id string) (session.Session, error) {
				lookups++
				require.Equal(t, "saved", id)
				return tc.result, tc.lookupErr
			})
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantID, saved.ID)
			require.Equal(t, tc.lookups, lookups)
		})
	}
}

func resumeLastFixture(t *testing.T, name string) *config.Config {
	t.Helper()
	root := t.TempDir()
	catalog := session.NewCatalog(root, "")
	if name != "absent" {
		require.NoError(t, catalog.SaveDraft("saved", "draft"))
	}
	if name == "archived" {
		require.NoError(t, catalog.Archive("saved", true))
	}
	return &config.Config{WorkingDir: root}
}

func TestExplicitResumePreservesMissingSessionError(t *testing.T) {
	_, err := resolveResumeSession(context.Background(), &config.Config{}, "missing", func(context.Context, string) (session.Session, error) {
		return session.Session{}, sql.ErrNoRows
	})
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestResumeRejectsMalformedArguments(t *testing.T) {
	for _, args := range [][]string{{"--resume="}, {"--resume", "one", "two"}, {"unrecognized"}} {
		command := &cobra.Command{Use: "opcode", SilenceErrors: true, SilenceUsage: true}
		addResumeFlag(command)
		command.RunE = func(*cobra.Command, []string) error { t.Fatal("invalid arguments reached startup"); return nil }
		command.SetArgs(args)
		require.Error(t, command.Execute())
	}
}

func TestResumeLastNeverOpensAnotherProjectCatalog(t *testing.T) {
	root, sharedData := t.TempDir(), t.TempDir()
	require.NoError(t, session.NewCatalog(root, sharedData).SaveDraft("other-project", "draft"))
	cfg := &config.Config{WorkingDir: t.TempDir(), Data: config.Data{Directory: sharedData}}
	_, err := resolveResumeSession(context.Background(), cfg, "last", func(context.Context, string) (session.Session, error) {
		t.Fatal("another project's session must never be loaded")
		return session.Session{}, nil
	})
	require.ErrorContains(t, err, "workspace mismatch")
}
