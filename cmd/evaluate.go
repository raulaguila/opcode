package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/evaluation"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/spf13/cobra"
)

func newEvaluateCommand() *cobra.Command {
	var casesPath, outputPath, providerPath string
	var live, allowExecution bool
	command := &cobra.Command{Use: "evaluate", Short: "Validate reproducible fixtures, or explicitly run a real provider in disposable workspaces", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		command.SilenceUsage = true
		suite, raw, err := evaluation.Load(casesPath)
		if err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		if outputPath != "" {
			output, _ := filepath.Abs(outputPath)
			for _, input := range []string{casesPath, providerPath} {
				if input != "" {
					source, _ := filepath.Abs(input)
					if output == source {
						return fmt.Errorf("report cannot overwrite an input configuration")
					}
				}
			}
		}
		report, err := evaluation.Run(command.Context(), suite, raw, evaluation.Options{Live: live, AllowExecution: allowExecution, ProviderConfig: providerPath, Executable: executable})
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if outputPath != "" {
			if err := localstate.WriteBytes(outputPath, append(encoded, '\n')); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(command.OutOrStdout(), string(encoded))
		}
		if evaluation.Failed(report) {
			return fmt.Errorf("one or more live evaluation criteria failed; inspect the report")
		}
		return nil
	}}
	command.Flags().StringVar(&casesPath, "cases", "", "JSON suite (default: six bundled fixtures)")
	command.Flags().StringVar(&outputPath, "output", "", "Write a private JSON report")
	command.Flags().BoolVar(&live, "live", false, "Explicitly allow real provider requests (may incur costs)")
	command.Flags().BoolVar(&allowExecution, "allow-execution", false, "Allow approved tools inside disposable execution fixtures; sandbox remains enforced")
	command.Flags().StringVar(&providerPath, "provider-config", "", "Provider/model JSON used only with --live; credentials are never printed")
	return command
}
func init() {
	rootCmd.AddCommand(newEvaluateCommand())
	var job string
	worker := &cobra.Command{Use: "evaluate-worker", Hidden: true, Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		command.SilenceUsage = true
		return evaluation.Worker(command.Context(), job, command.OutOrStdout())
	}}
	worker.Flags().StringVar(&job, "job", "", "Internal bounded evaluation job")
	rootCmd.AddCommand(worker)
}
