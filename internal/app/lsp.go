package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/lsp/watcher"
)

func (app *App) initLSPClients(parent context.Context) {
	lifetime, cancel := context.WithCancel(parent)
	app.lspStop = cancel
	// Remember only configuration fingerprints, never executable arguments.
	unavailable := map[string][32]byte{}
	app.lspUnregister = lsp.RegisterLazyClients(app.LSPClients, func(ctx context.Context, path string) error {
		return app.ensureLSPClients(ctx, lifetime, path, unavailable)
	})
}

// createAndStartLSPClient creates a new LSP client, initializes it, and starts its workspace watcher
func (app *App) createAndStartLSPClient(ctx context.Context, name string, command string, args ...string) {
	// Create a specific context for initialization with a timeout
	label := lsp.ExecutableLabel(command)
	logging.Info(config.Text("Iniciando LSP", "Starting LSP"), "server", label)

	// Create the LSP client
	lspClient, err := lsp.NewClient(ctx, command, args...)
	if err != nil {
		if errors.Is(err, lsp.ErrExecutableUnavailable) {
			logging.Warn(err.Error())
		} else if ctx.Err() == nil {
			logging.Warn(config.Text("LSP não iniciou; veja /integrations", "LSP failed to start; see /integrations"), "server", label)
		}
		return
	}

	// Create a longer timeout for initialization (some servers take time to start)
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Initialize with the initialization context
	_, err = lspClient.InitializeLSPClient(initCtx, config.WorkingDirectory())
	if err != nil {
		if ctx.Err() == nil {
			logging.Warn(config.Text("LSP não inicializou; veja /integrations", "LSP initialization failed; see /integrations"), "server", label)
		}
		// Clean up the client to prevent resource leaks
		lspClient.Close()
		return
	}

	// Wait for the server to be ready
	if err := lspClient.WaitForServerReady(initCtx); err != nil {
		if ctx.Err() == nil {
			logging.Warn(config.Text("LSP ainda indisponível; veja /integrations", "LSP not ready; see /integrations"), "server", label)
		}
		// We'll continue anyway, as some functionality might still work
		lspClient.SetServerState(lsp.StateError)
	} else {
		logging.Info(config.Text("LSP pronto", "LSP ready"), "server", label)
		lspClient.SetServerState(lsp.StateReady)
	}

	if ctx.Err() != nil {
		lspClient.Close()
		return
	}
	logging.Info(config.Text("LSP inicializado", "LSP initialized"), "server", label)

	// Create a child context that can be canceled when the app is shutting down
	watchCtx, cancelFunc := context.WithCancel(ctx)

	// Create a context with the server name for better identification
	watchCtx = watcher.WithServerName(watchCtx, name)

	// Create the workspace watcher
	workspaceWatcher := watcher.NewWorkspaceWatcher(lspClient)

	// Store the cancel function to be called during cleanup
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancelFunc)
	app.cancelFuncsMutex.Unlock()

	// Add the watcher to a WaitGroup to track active goroutines
	app.watcherWG.Add(1)

	// Add to map with mutex protection before starting goroutine
	app.clientsMutex.Lock()
	lsp.SetClient(app.LSPClients, name, lspClient)
	app.clientsMutex.Unlock()

	go app.runWorkspaceWatcher(watchCtx, name, workspaceWatcher)
}

// runWorkspaceWatcher executes the workspace watcher for an LSP client
func (app *App) runWorkspaceWatcher(ctx context.Context, name string, workspaceWatcher *watcher.WorkspaceWatcher) {
	defer app.watcherWG.Done()
	defer logging.RecoverPanic("LSP-"+name, func() {
		// Try to restart the client
		app.restartLSPClient(ctx, name)
	})

	workspaceWatcher.WatchWorkspace(ctx, config.WorkingDirectory())
	logging.Info("Workspace watcher stopped", "client", name)
}

// restartLSPClient attempts to restart a crashed or failed LSP client
func (app *App) restartLSPClient(ctx context.Context, name string) {
	// Get the original configuration
	cfg := config.Get()
	clientConfig, exists := cfg.LSP[name]
	if !exists {
		logging.Error("Cannot restart client, configuration not found", "client", name)
		return
	}

	// Clean up the old client if it exists
	app.clientsMutex.Lock()
	oldClient, exists := app.LSPClients[name]
	if exists {
		lsp.DeleteClient(app.LSPClients, name) // Remove from map before potentially slow shutdown
	}
	app.clientsMutex.Unlock()

	if exists && oldClient != nil {
		// Try to shut it down gracefully, but don't block on errors
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = oldClient.Shutdown(shutdownCtx)
		cancel()
	}

	// Create a new client using the shared function
	app.createAndStartLSPClient(ctx, name, clientConfig.Command, clientConfig.Args...)
	logging.Info("Successfully restarted LSP client", "client", name)
}

func checkLazyLSPExecutable(entry config.LSPConfig, name, directory string, unavailable map[string][32]byte) error {
	if err := lsp.CheckExecutable(entry.Command, directory); err != nil {
		data, _ := json.Marshal(entry)
		fingerprint := sha256.Sum256(data)
		if previous, seen := unavailable[name]; !seen || previous != fingerprint {
			logging.Warn(err.Error())
			unavailable[name] = fingerprint
		}
		return err
	}
	return nil
}

func (app *App) startLazyLSPClient(ctx, lifetime context.Context, name string, entry config.LSPConfig) error {
	startCtx, stop := context.WithCancel(lifetime)
	done := context.AfterFunc(ctx, stop)
	app.createAndStartLSPClient(startCtx, name, entry.Command, entry.Args...)
	done()
	if ctx.Err() != nil {
		if client := lsp.SnapshotClients(app.LSPClients)[name]; client != nil {
			client.Close()
			lsp.DeleteClient(app.LSPClients, name)
		}
		stop()
		return ctx.Err()
	}
	if lsp.SnapshotClients(app.LSPClients)[name] == nil {
		stop()
		return fmt.Errorf("%s: %s", lsp.ExecutableLabel(entry.Command), config.Text("falha ao iniciar; veja /integrations", "failed to start; see /integrations"))
	}
	return nil
}

func (app *App) ensureLSPClients(ctx, lifetime context.Context, path string, unavailable map[string][32]byte) error {
	app.lspStartMu.Lock()
	defer app.lspStartMu.Unlock()
	if err := lifetime.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := config.Get()
	keys := []string{}
	for name := range cfg.LSP {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		entry := cfg.LSP[name]
		if entry.Disabled || !lsp.ServerMatches(entry, path) {
			continue
		}
		if lsp.SnapshotClients(app.LSPClients)[name] != nil {
			continue
		}
		if err := checkLazyLSPExecutable(entry, name, cfg.WorkingDir, unavailable); err != nil {
			return err
		}
		delete(unavailable, name)
		if err := app.startLazyLSPClient(ctx, lifetime, name, entry); err != nil {
			return err
		}
	}
	return nil
}
