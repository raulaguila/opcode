package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newConfigCommand() *cobra.Command {
	var file, cwd string
	root := &cobra.Command{Use: "config", Short: "Locate, validate and safely migrate configuration without inference"}
	root.PersistentFlags().StringVar(&file, "file", "", "Explicit configuration file")
	root.PersistentFlags().StringVarP(&cwd, "cwd", "c", "", "Project directory")
	paths := func() (string, string, error) {
		directory := cwd
		if directory == "" {
			directory, _ = os.Getwd()
		}
		directory, err := filepath.Abs(directory)
		g, p := config.Paths(directory)
		return g, p, err
	}
	root.AddCommand(&cobra.Command{Use: "path", Args: cobra.NoArgs, Short: "Show selected global and project paths without creating files", RunE: func(cmd *cobra.Command, _ []string) error {
		return runConfigPath(cmd, file, paths)
	}})
	root.AddCommand(&cobra.Command{Use: "validate", Args: cobra.NoArgs, Short: "Validate JSON and known fields offline; does not require a model", RunE: func(cmd *cobra.Command, _ []string) error {
		return runConfigValidate(cmd, file, paths)
	}})
	var redacted bool
	show := &cobra.Command{Use: "show", Args: cobra.NoArgs, Short: "Show allowlisted effective values and origins; secrets are always omitted", RunE: func(cmd *cobra.Command, _ []string) error {
		return runConfigShow(cmd, file, cwd, redacted)
	}}
	show.Flags().BoolVar(&redacted, "redacted", true, "Keep credentials and arbitrary extension values omitted (always enabled)")
	root.AddCommand(show)
	root.AddCommand(&cobra.Command{Use: "migrate", Args: cobra.NoArgs, Short: "Add schemaVersion in place after saving an exact private backup", RunE: func(cmd *cobra.Command, _ []string) error {
		return runConfigMigrate(cmd, file, paths)
	}})
	return root
}
func init() { rootCmd.AddCommand(newConfigCommand()) }

func runConfigPath(cmd *cobra.Command, file string, paths func() (string, string, error)) error {

	g, p, e := paths()
	if e != nil {
		return e
	}
	if file != "" {
		g = file
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"global": g, "project": p})
}

func runConfigValidate(cmd *cobra.Command, file string, paths func() (string, string, error)) error {

	cmd.SilenceUsage = true
	g, p, e := paths()
	if e != nil {
		return e
	}
	targets := []string{g, p}
	if file != "" {
		if _, err := os.Lstat(file); err != nil {
			return fmt.Errorf("explicit configuration file is unavailable")
		}
		targets = []string{file}
	}
	for _, path := range targets {
		if err := config.ValidateDocument(path); err != nil {
			return err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Fprintf(cmd.OutOrStdout(), "Absent (optional): %s\n", path)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Valid: %s\n", path)
		}
	}
	return nil
}

func runConfigShow(cmd *cobra.Command, file, cwd string, redacted bool) error {

	cmd.SilenceUsage = true
	if file != "" {
		viper.SetConfigFile(file)
	}
	if !redacted {
		return fmt.Errorf("configuration output always requires --redacted")
	}
	directory := cwd
	if directory == "" {
		directory, _ = os.Getwd()
	}
	if _, err := config.LoadOffline(directory); err != nil {
		return fmt.Errorf("configuration could not be loaded; use config validate for field diagnostics")
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(config.RedactedDocument())
}

func runConfigMigrate(cmd *cobra.Command, file string, paths func() (string, string, error)) error {

	cmd.SilenceUsage = true
	g, _, e := paths()
	if e != nil {
		return e
	}
	if file != "" {
		g = file
	}
	backup, changed, e := config.MigrateDocument(g)
	if e != nil {
		return e
	}
	if changed {
		fmt.Fprintf(cmd.OutOrStdout(), "Migrated in place: %s\nPrivate backup: %s\n", g, backup)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Configuration is already current.")
	}
	return nil
}
