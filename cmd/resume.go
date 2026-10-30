package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/spf13/cobra"
)

func addResumeFlag(command *cobra.Command) {
	command.Flags().String("resume", "", "Resume the last project session, or a saved session ID (also works with --prompt)")
	command.Flags().Lookup("resume").NoOptDefVal = "last"
	command.Args = func(command *cobra.Command, args []string) error {
		if !command.Flags().Changed("resume") {
			return cobra.NoArgs(command, args)
		}
		_, err := resumeRequest(command, args)
		return err
	}
}

func resumeRequest(command *cobra.Command, args []string) (string, error) {
	id, err := command.Flags().GetString("resume")
	if err != nil || !command.Flags().Changed("resume") {
		return id, err
	}
	// Optional flag values are positional in pflag; retain --resume ID compatibility.
	if id == "last" && len(args) == 1 {
		return args[0], nil
	}
	if id == "" || len(args) != 0 {
		return "", fmt.Errorf("use --resume, --resume ID or --resume=ID")
	}
	return id, nil
}

func resolveResumeSession(ctx context.Context, cfg *config.Config, requested string, get func(context.Context, string) (session.Session, error)) (session.Session, error) {
	if requested == "" {
		return session.Session{}, nil
	}
	if requested != "last" {
		return get(ctx, requested)
	}
	metadata, id, err := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory).Snapshot()
	if err != nil {
		return session.Session{}, err
	}
	if id == "" || metadata[id].Archived {
		return session.Session{}, nil
	}
	saved, err := get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && saved.ParentSessionID != "") {
		return session.Session{}, nil
	}
	return saved, err
}
