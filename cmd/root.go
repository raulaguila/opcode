package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/format"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui"
	"github.com/raulaguila/opcode/internal/version"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "opcode",
	Short: "Terminal-based AI assistant for software development",
	Long: `Opcode is a powerful terminal-based AI assistant that helps with software development tasks.
It provides an interactive chat interface with AI capabilities, code analysis, and LSP integration
to assist developers in writing, debugging, and understanding code directly from the terminal.`,
	Example: `
  # Run in interactive mode
  opcode

  # Run with debug logging
  opcode -d

  # Run with debug logging in a specific directory
  opcode -d -c /path/to/project

  # Print version
  opcode -v

  # Run a single non-interactive prompt
  opcode -p "Explain the use of context in Go"

  # Run a single non-interactive prompt with JSON output format
  opcode -p "Explain the use of context in Go" -f json
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		// If the help flag is set, show the help message
		if cmd.Flag("help").Changed {
			cmd.Help()
			return nil
		}
		if cmd.Flag("version").Changed {
			fmt.Println(version.Version)
			return nil
		}

		// Load the config
		debug, _ := cmd.Flags().GetBool("debug")
		cwd, _ := cmd.Flags().GetString("cwd")
		prompt, _ := cmd.Flags().GetString("prompt")
		outputFormat, _ := cmd.Flags().GetString("output-format")
		quiet, _ := cmd.Flags().GetBool("quiet")
		autoApprove, _ := cmd.Flags().GetBool("auto-approve")
		if kind, _ := cmd.Flags().GetString("task-kind"); kind == "" && (cmd.Flags().Changed("target") || cmd.Flags().Changed("accept") || cmd.Flags().Changed("check")) {
			return fmt.Errorf("--target, --accept and --check require --task-kind")
		}
		if autoApprove && prompt == "" {
			return fmt.Errorf("--auto-approve requires a non-interactive --prompt")
		}

		// Validate format option
		if !format.IsValid(outputFormat) {
			return fmt.Errorf("invalid format option: %s\n%s", outputFormat, format.GetHelpText())
		}

		if cwd != "" {
			err := os.Chdir(cwd)
			if err != nil {
				return fmt.Errorf("failed to change directory: %v", err)
			}
		}
		if cwd == "" {
			c, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %v", err)
			}
			cwd = c
		}
		// Runtime setup errors should explain the problem without flag usage.
		cmd.SilenceUsage = true
		loadedConfig, err := config.Load(cwd, debug)
		if err != nil {
			return err
		}
		if prompt != "" {
			if err := loadedConfig.ValidateStartup(); err != nil {
				return err
			}
		}

		if cmd.Flags().Changed("mode") {
			loadedConfig.Runtime.Mode, _ = cmd.Flags().GetString("mode")
		}
		for _, entry := range []struct {
			flag   string
			target *int
		}{{"max-steps", &loadedConfig.Runtime.MaxSteps}, {"max-seconds", &loadedConfig.Runtime.MaxSeconds}, {"tool-budget", &loadedConfig.Runtime.ToolBudget}, {"first-token-timeout", &loadedConfig.Runtime.FirstTokenTimeout}, {"idle-timeout", &loadedConfig.Runtime.IdleTimeout}} {
			if cmd.Flags().Changed(entry.flag) {
				value, _ := cmd.Flags().GetInt(entry.flag)
				if value < 1 {
					return fmt.Errorf("--%s must be positive", entry.flag)
				}
				*entry.target = value
			}
		}
		if err := loadedConfig.Runtime.Validate(); err != nil {
			return err
		}
		resumeID, _ := cmd.Flags().GetString("resume")
		// Connect DB, this will also run migrations
		conn, err := db.Connect()
		if err != nil {
			return err
		}

		// Create main context for the application
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		app, err := app.New(ctx, conn)
		if err != nil {
			logging.Error("Failed to create app: %v", err)
			return err
		}
		defer app.Shutdown()
		kind, _ := cmd.Flags().GetString("task-kind")
		if kind != "" {
			targets, _ := cmd.Flags().GetStringArray("target")
			criteria, _ := cmd.Flags().GetStringArray("accept")
			checks, _ := cmd.Flags().GetStringArray("check")
			contract := &workflow.Contract{Kind: kind, Targets: targets, Acceptance: criteria, Checks: checks}
			if err := contract.Validate(); err != nil {
				return err
			}
			if prompt == "" {
				return fmt.Errorf("--task-kind requires --prompt; use /contract in interactive mode")
			}
			for _, name := range checks {
				if loadedConfig.Checks[name].Command == "" {
					return fmt.Errorf("--check must name a configured check")
				}
			}
			app.InitialContract = contract
		}

		var initialSessions []session.Session
		if resumeID != "" {
			saved, err := app.Sessions.Get(ctx, resumeID)
			if err != nil {
				return fmt.Errorf("resume session: %w", err)
			}
			if _, err := app.Workflows.Get(ctx, resumeID); err != nil {
				return err
			}
			if cmd.Flags().Changed("mode") {
				if err := app.CoderAgent.SetMode(ctx, resumeID, workflow.Mode(loadedConfig.Runtime.Mode)); err != nil {
					return err
				}
			}
			initialSessions = append(initialSessions, saved)
		}
		// Initialize MCP tools early for both modes
		initMCPTools(ctx, app)

		// Non-interactive mode
		if prompt != "" {
			// Run non-interactive flow using the App method
			return app.RunNonInteractive(ctx, prompt, outputFormat, quiet, autoApprove, resumeID)
		}

		// Interactive mode
		// Set up the TUI
		zone.NewGlobal()
		keyboard := tui.NewKeyboard(os.Stdout)
		defer keyboard.Close()
		program := tea.NewProgram(
			tui.New(app, initialSessions...),
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
			tea.WithReportFocus(),
			tea.WithFilter(keyboard.Filter),
			tea.WithOutput(keyboard),
		)

		// Setup the subscriptions, this will send services events to the TUI
		ch, cancelSubs := setupSubscriptions(app, ctx)

		// Create a context for the TUI message handler
		tuiCtx, tuiCancel := context.WithCancel(ctx)
		var tuiWg sync.WaitGroup
		tuiWg.Add(1)

		// Set up message handling for the TUI
		go func() {
			defer tuiWg.Done()
			defer logging.RecoverPanic("TUI-message-handler", func() {
				attemptTUIRecovery(program)
			})

			for {
				select {
				case <-tuiCtx.Done():
					logging.Info("TUI message handler shutting down")
					return
				case msg, ok := <-ch:
					if !ok {
						logging.Info("TUI message channel closed")
						return
					}
					program.Send(msg)
				}
			}
		}()

		// Cleanup function for when the program exits
		cleanup := func() {
			// Shutdown the app
			app.Shutdown()

			// Cancel subscriptions first
			cancelSubs()

			// Then cancel TUI message handler
			tuiCancel()

			// Wait for TUI message handler to finish
			tuiWg.Wait()

			logging.Info("All goroutines cleaned up")
		}

		// Run the TUI
		result, err := program.Run()
		cleanup()

		if err != nil {
			logging.Error("TUI error: %v", err)
			return fmt.Errorf("TUI error: %v", err)
		}

		logging.Info("TUI exited with result: %v", result)
		return nil
	},
}

// attemptTUIRecovery tries to recover the TUI after a panic
func attemptTUIRecovery(program *tea.Program) {
	logging.Info("Attempting to recover TUI after panic")

	// We could try to restart the TUI or gracefully exit
	// For now, we'll just quit the program to avoid further issues
	program.Quit()
}

func initMCPTools(ctx context.Context, app *app.App) {
	go func() {
		defer logging.RecoverPanic("MCP-goroutine", nil)

		// Create a context with timeout for the initial MCP tools fetch
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Set this up once with proper error handling
		agent.GetMcpTools(ctxWithTimeout, app.Permissions)
		logging.Info("MCP message handling goroutine exiting")
	}()
}

func setupSubscriber[T any](
	ctx context.Context,
	wg *sync.WaitGroup,
	name string,
	subscriber func(context.Context) <-chan pubsub.Event[T],
	outputCh chan<- tea.Msg,
) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer logging.RecoverPanic(fmt.Sprintf("subscription-%s", name), nil)

		subCh := subscriber(ctx)

		for {
			select {
			case event, ok := <-subCh:
				if !ok {
					logging.Info("subscription channel closed", "name", name)
					return
				}

				var msg tea.Msg = event

				select {
				case outputCh <- msg:
				case <-time.After(2 * time.Second):
					logging.Warn("message dropped due to slow consumer", "name", name)
				case <-ctx.Done():
					logging.Info("subscription cancelled", "name", name)
					return
				}
			case <-ctx.Done():
				logging.Info("subscription cancelled", "name", name)
				return
			}
		}
	}()
}

func setupSubscriptions(app *app.App, parentCtx context.Context) (chan tea.Msg, func()) {
	ch := make(chan tea.Msg, 100)

	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(parentCtx) // Inherit from parent context

	setupSubscriber(ctx, &wg, "logging", logging.Subscribe, ch)
	setupSubscriber(ctx, &wg, "sessions", app.Sessions.Subscribe, ch)
	setupSubscriber(ctx, &wg, "messages", app.Messages.Subscribe, ch)
	setupSubscriber(ctx, &wg, "permissions", app.Permissions.Subscribe, ch)
	setupSubscriber(ctx, &wg, "coderAgent", app.CoderAgent.Subscribe, ch)

	var cleanupOnce sync.Once
	cleanupFunc := func() { cleanupOnce.Do(func() { stopSubscriptions(cancel, &wg, ch, 5*time.Second) }) }

	return ch, cleanupFunc
}

// The waiter exclusively owns closing the output: a timeout cannot prove that
// all publishers have stopped. Late publishers must never encounter a closed
// channel while the caller continues shutting down.
func stopSubscriptions(cancel context.CancelFunc, wg *sync.WaitGroup, ch chan tea.Msg, timeout time.Duration) {
	logging.Info("Cancelling all subscriptions")
	cancel()
	stopped := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
		close(stopped)
	}()
	select {
	case <-stopped:
		logging.Info("All subscription goroutines completed successfully")
	case <-time.After(timeout):
		logging.Warn("Timed out waiting for subscriptions; output will close after publishers finish")
	}
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolP("help", "h", false, "Help")
	rootCmd.Flags().String("task-kind", "", "Explicit outcome: explain, plan, change, verify (with --prompt)")
	rootCmd.Flags().StringArray("target", nil, "Relative task target (repeatable; advisory, never permission)")
	rootCmd.Flags().StringArray("accept", nil, "Acceptance criterion (repeatable)")
	rootCmd.Flags().StringArray("check", nil, "Required configured check (repeatable)")
	rootCmd.Flags().BoolP("version", "v", false, "Version")
	rootCmd.Flags().BoolP("debug", "d", false, "Debug")
	rootCmd.Flags().StringP("cwd", "c", "", "Current working directory")
	rootCmd.Flags().StringP("prompt", "p", "", "Prompt to run in non-interactive mode")

	// Add format flag with validation logic
	rootCmd.Flags().StringP("output-format", "f", format.Text.String(),
		"Output format for non-interactive mode (text, json)")

	// Add quiet flag to hide spinner in non-interactive mode
	rootCmd.Flags().BoolP("quiet", "q", false, "Hide spinner in non-interactive mode")
	rootCmd.Flags().String("mode", "", "Initial mode: ask, plan or execute")
	rootCmd.Flags().String("resume", "", "Resume a saved session ID (also works with --prompt)")
	rootCmd.Flags().Int("max-steps", 0, "Maximum model requests per turn (1–200)")
	rootCmd.Flags().Int("max-seconds", 0, "Maximum turn duration including user waits (seconds, 1–7200)")
	rootCmd.Flags().Int("tool-budget", 0, "Maximum bytes of tool results per turn (4096–1048576)")
	rootCmd.Flags().Int("first-token-timeout", 0, "Maximum wait for initial model output (seconds, 1–1800)")
	rootCmd.Flags().Int("idle-timeout", 0, "Maximum model streaming inactivity (seconds, 1–600)")
	rootCmd.Flags().Bool("auto-approve", false, "Approve all tool permissions for a non-interactive prompt (unsafe)")

	// Register custom validation for the format flag
	rootCmd.RegisterFlagCompletionFunc("output-format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return format.SupportedFormats, cobra.ShellCompDirectiveNoFileComp
	})
}
