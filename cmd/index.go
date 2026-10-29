package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/spf13/cobra"
)

func newIndexCommand() *cobra.Command {
	command := &cobra.Command{Use: "index [status|update|rebuild|pause|resume|clear|explain] [path]", Short: "Manage derived project index without model inference", Args: cobra.RangeArgs(0, 2)}
	command.RunE = func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		cwd, _ := cmd.Flags().GetString("cwd")
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		cwd, err := filepath.Abs(cwd)
		if err != nil {
			return err
		}
		if _, err := config.LoadOffline(cwd); err != nil {
			return err
		}
		action, path := "status", ""
		if len(args) > 0 {
			action = args[0]
		}
		if len(args) > 1 {
			path = args[1]
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		text, err := tools.IndexStatusJSON(ctx, action, path)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), text)
		return err
	}
	command.Flags().StringP("cwd", "c", "", "Project directory")
	return command
}
func init() { rootCmd.AddCommand(newIndexCommand()) }
