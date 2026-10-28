package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/format"
	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/workflow"
)

type App struct {
	Sessions    session.Service
	Messages    message.Service
	History     history.Service
	Permissions permission.Service

	CoderAgent agent.Service
	Processes  *tools.ProcessManager
	Workflows  *workflow.Store
	Questions  *interaction.Questions

	LSPClients map[string]*lsp.Client

	clientsMutex sync.RWMutex

	watcherCancelFuncs []context.CancelFunc
	cancelFuncsMutex   sync.Mutex
	coderSetupMu       sync.Mutex
	watcherWG          sync.WaitGroup
}

func New(ctx context.Context, conn *sql.DB) (*App, error) {
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	files := history.NewService(q, conn)

	app := &App{
		Sessions:    sessions,
		Messages:    messages,
		History:     files,
		Permissions: permission.NewPermissionService(),
		LSPClients:  make(map[string]*lsp.Client),
		Processes:   tools.NewProcessManager(),
		Workflows:   workflow.New(conn, config.WorkingDirectory(), workflow.Mode(config.Get().Runtime.Defaults().Mode)),
		Questions:   interaction.NewQuestions(),
	}

	// Initialize theme based on configuration
	app.initTheme()

	// Initialize LSP clients in the background
	app.initLSPClients(ctx)

	app.CoderAgent = newConfigurableCoder(ctx, app)
	if config.Get().Agents[config.AgentCoder].Model != "" {
		if err := app.ConfigureCoderAgent(); err != nil {
			app.Shutdown()
			return nil, err
		}
	}

	return app, nil
}

// ConfigureCoderAgent initializes or rebuilds the idle agent after settings
// change. The stable service keeps UI subscriptions attached during first setup.
func (app *App) ConfigureCoderAgent() error {
	app.coderSetupMu.Lock()
	defer app.coderSetupMu.Unlock()
	if app.CoderAgent != nil && app.CoderAgent.IsBusy() {
		return fmt.Errorf("cannot change provider while processing requests")
	}
	if err := config.Get().ValidateStartup(); err != nil {
		return err
	}
	candidate, err := agent.NewAgent(config.AgentCoder, app.Sessions, app.Messages,
		agent.CoderAgentTools(app.Permissions, app.Sessions, app.Messages, app.History, app.LSPClients, app.Processes),
		agent.WithWorkflow(app.Workflows, app.Questions),
		agent.WithModeChange(func(id string) { app.Processes.CancelSession(id); app.Permissions.RevokeSession(id) }))
	if err != nil {
		return err
	}
	if proxy, ok := app.CoderAgent.(*configurableCoder); ok {
		return proxy.bind(candidate)
	}
	previous := app.CoderAgent
	app.CoderAgent = candidate
	if closer, ok := previous.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	return nil
}

// initTheme sets the application theme based on the configuration
func (app *App) initTheme() {
	cfg := config.Get()
	if cfg == nil || cfg.TUI.Theme == "" {
		return // Use default theme
	}

	// Try to set the theme from config
	err := theme.SetTheme(cfg.TUI.Theme)
	if err != nil {
		logging.Warn("Failed to set theme from config, using default theme", "theme", cfg.TUI.Theme, "error", err)
	} else {
		logging.Debug("Set theme from config", "theme", cfg.TUI.Theme)
	}
}

// RunNonInteractive handles the execution flow when a prompt is provided via CLI flag.
func (a *App) RunNonInteractive(ctx context.Context, prompt string, outputFormat string, quiet, autoApprove bool, resumeIDs ...string) error {
	logging.Info("Running in non-interactive mode")

	// Start spinner if not in quiet mode
	var spinner *format.Spinner
	if !quiet {
		spinner = format.NewSpinner("Thinking...")
		spinner.Start()
		defer spinner.Stop()
	}

	const maxPromptLengthForTitle = 100
	titlePrefix := "Non-interactive: "
	var titleSuffix string

	if len(prompt) > maxPromptLengthForTitle {
		titleSuffix = prompt[:maxPromptLengthForTitle] + "..."
	} else {
		titleSuffix = prompt
	}
	title := titlePrefix + titleSuffix

	var sess session.Session
	var err error
	if len(resumeIDs) > 0 && resumeIDs[0] != "" {
		sess, err = a.Sessions.Get(ctx, resumeIDs[0])
	} else {
		sess, err = a.Sessions.Create(ctx, title)
	}
	if err != nil {
		return fmt.Errorf("failed to create session for non-interactive mode: %w", err)
	}
	logging.Info("Created session for non-interactive run", "session_id", sess.ID)

	if autoApprove {
		a.Permissions.AutoApproveSession(sess.ID)
	}

	done, err := a.CoderAgent.Run(ctx, sess.ID, prompt)
	if err != nil {
		return fmt.Errorf("failed to start agent processing stream: %w", err)
	}

	result := <-done
	if result.Error != nil {
		if errors.Is(result.Error, context.Canceled) || errors.Is(result.Error, agent.ErrRequestCancelled) {
			logging.Info("Agent processing cancelled", "session_id", sess.ID)
			return nil
		}
		return fmt.Errorf("agent processing failed: %w", result.Error)
	}

	// Stop spinner before printing output
	if !quiet && spinner != nil {
		spinner.Stop()
	}

	// Get the text content from the response
	content := "No content available"
	if result.Message.Content().String() != "" {
		content = result.Message.Content().String()
	}

	fmt.Println(format.FormatOutput(content, outputFormat))

	logging.Info("Non-interactive run completed", "session_id", sess.ID)

	return nil
}

// Shutdown performs a clean shutdown of the application
func (app *App) Shutdown() {
	if closer, ok := app.CoderAgent.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			logging.Error("failed to stop tool processes", "error", err)
		}
	}
	if app.Processes != nil {
		_ = app.Processes.Close()
	}
	// Cancel all watcher goroutines
	app.cancelFuncsMutex.Lock()
	for _, cancel := range app.watcherCancelFuncs {
		cancel()
	}
	app.cancelFuncsMutex.Unlock()
	app.watcherWG.Wait()

	// Perform additional cleanup for LSP clients
	app.clientsMutex.RLock()
	clients := make(map[string]*lsp.Client, len(app.LSPClients))
	maps.Copy(clients, app.LSPClients)
	app.clientsMutex.RUnlock()

	for name, client := range clients {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := client.Shutdown(shutdownCtx); err != nil {
			logging.Error("Failed to shutdown LSP client", "name", name, "error", err)
		}
		cancel()
	}
}
