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
	var casesPath, outputPath, providerPath, adapterPath, experiment string
	var contextWindow int64
	var live, allowExecution bool
	var repeat int
	var caseIDs []string
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
		var adapter *evaluation.Adapter
		if adapterPath != "" {
			adapter, err = evaluation.LoadAdapter(adapterPath)
			if err != nil {
				return err
			}
		}
		report, err := evaluation.Run(command.Context(), suite, raw, evaluation.Options{Experiment: experiment, Adapter: adapter, ContextWindow: contextWindow, Repeats: repeat, CaseIDs: caseIDs, Live: live, AllowExecution: allowExecution, ProviderConfig: providerPath, Executable: executable})
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
			return fmt.Errorf("one or more live evaluation criteria failed or are unavailable; inspect the report")
		}
		return nil
	}}
	command.Flags().StringVar(&adapterPath, "adapter", "", "Explicit trusted JSON adapter executable (opcode/codaro/crush); requires --live and --context-window")
	command.Flags().StringVar(&experiment, "experiment", "", "Report label for an observed prompt/config variant; does not override prompts")
	command.Flags().Int64Var(&contextWindow, "context-window", 0, "Explicit shared comparison context; 0 uses normal provider discovery for native Opcode")
	command.Flags().IntVar(&repeat, "repeat", 1, "Repeat live cases 1–3 times in fresh workspaces (multiplies inference requests)")
	command.Flags().StringSliceVar(&caseIDs, "case", nil, "Run only these bundled/custom case IDs (comma separated); criteria stay unchanged")
	command.Flags().StringVar(&casesPath, "cases", "", "JSON suite (default: six bundled fixtures)")
	command.Flags().StringVar(&outputPath, "output", "", "Write a private JSON report")
	command.Flags().BoolVar(&live, "live", false, "Explicitly allow real provider requests (may incur costs)")
	command.Flags().BoolVar(&allowExecution, "allow-execution", false, "Allow approved tools inside disposable execution fixtures; sandbox remains enforced")
	command.Flags().StringVar(&providerPath, "provider-config", "", "Provider/model JSON used only with --live; credentials are never printed")
	return command
}
func init() {
	evaluate := newEvaluateCommand()
	rootCmd.AddCommand(evaluate)
	var comparisonOutput string
	compare := &cobra.Command{Use: "compare REPORT REPORT [REPORT...]", Short: "Compare observed live reports; mark differing conditions and unavailable measurements", Args: cobra.RangeArgs(2, 12), RunE: func(command *cobra.Command, args []string) error {
		command.SilenceUsage = true
		for _, input := range args {
			if comparisonOutput != "" {
				out, _ := filepath.Abs(comparisonOutput)
				in, _ := filepath.Abs(input)
				if out == in {
					return fmt.Errorf("comparison cannot overwrite its input")
				}
			}
		}
		report, err := evaluation.Compare(args)
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if comparisonOutput != "" {
			return localstate.WriteBytes(comparisonOutput, append(raw, '\n'))
		}
		fmt.Fprintln(command.OutOrStdout(), string(raw))
		return nil
	}}
	compare.Flags().StringVar(&comparisonOutput, "output", "", "Write private JSON comparison")
	evaluate.AddCommand(compare)
	evaluate.AddCommand(newExperimentCommand())
	evaluate.AddCommand(newRetrievalCommand())
	var job string
	worker := &cobra.Command{Use: "evaluate-worker", Hidden: true, Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		command.SilenceUsage = true
		return evaluation.Worker(command.Context(), job, command.OutOrStdout())
	}}
	worker.Flags().StringVar(&job, "job", "", "Internal bounded evaluation job")
	rootCmd.AddCommand(worker)
	var rankingJob string
	rankingWorker := &cobra.Command{Use: "evaluate-retrieval-worker", Hidden: true, Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		command.SilenceUsage = true
		return evaluation.RetrievalWorker(command.Context(), rankingJob, command.OutOrStdout())
	}}
	rankingWorker.Flags().StringVar(&rankingJob, "job", "", "Internal bounded ranking evaluation job")
	rootCmd.AddCommand(rankingWorker)
}

func newExperimentCommand() *cobra.Command {
	var output string
	var live, execution bool
	command := &cobra.Command{Use: "experiment MANIFEST", Short: "Plan or explicitly execute one shared experiment across applications", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		command.SilenceUsage = true
		experiment, err := evaluation.LoadExperiment(args[0])
		if err != nil {
			return err
		}
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		report, err := experiment.Run(command.Context(), binary, output, live, execution)
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(command.OutOrStdout(), string(raw))
		if live && (report.Comparison == nil || !report.Comparison.Rankable) {
			return fmt.Errorf("experiment cannot be ranked: missing, unavailable or mismatched observations; inspect saved reports")
		}
		for _, app := range report.Applications {
			if live && app.Status != "completed" {
				return fmt.Errorf("one or more application criteria failed; reports preserved")
			}
		}
		return nil
	}}
	command.Flags().BoolVar(&live, "live", false, "Explicitly execute real providers; otherwise only validate and print a plan")
	command.Flags().BoolVar(&execution, "allow-execution", false, "Allow execution fixtures under their existing sandbox/approval restrictions")
	command.Flags().StringVar(&output, "output-dir", "", "New directory for live reports; never overwrite prior evidence")
	return command
}

func newRetrievalCommand() *cobra.Command {
	var cases, output, embedding string
	command := &cobra.Command{Use: "retrieval", Short: "Measure real search ranking in isolated fixtures, without chat inference", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		command.SilenceUsage = true
		for _, input := range []string{cases, embedding} {
			if input != "" && output != "" {
				a, _ := filepath.Abs(input)
				b, _ := filepath.Abs(output)
				if a == b {
					return fmt.Errorf("ranking report cannot overwrite its input")
				}
			}
		}
		suite, raw, err := evaluation.LoadRetrieval(cases)
		if err != nil {
			return err
		}
		embeddings, err := evaluation.LoadEmbeddingConfig(embedding)
		if err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		report, err := evaluation.RunRetrieval(command.Context(), suite, raw, executable, embeddings)
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if output != "" {
			if err := localstate.WriteBytes(output, append(encoded, '\n')); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(command.OutOrStdout(), string(encoded))
		}
		for _, result := range report.Results {
			if result.Status == "failed" {
				return fmt.Errorf("ranking tool failed; inspect the report")
			}
		}
		return nil
	}}
	command.Flags().StringVar(&cases, "cases", "", "Ranking fixture JSON (default: bundled symbol/scope/synonym cases)")
	command.Flags().StringVar(&output, "output", "", "Write private JSON ranking report")
	command.Flags().StringVar(&embedding, "embedding-config", "", "Explicit {endpoint,model} JSON; permits bounded local embedding HTTP calls, never model downloads")
	return command
}
