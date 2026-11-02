package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/desktop"
	"github.com/spf13/cobra"
)

func newAppCommand() *cobra.Command {
	var cwd string
	var noOpen bool
	c := &cobra.Command{Use: "app", Short: "Abre a interface gráfica numa janela própria", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		return runApp(c, cwd, noOpen)
	}}
	c.Flags().StringVarP(&cwd, "cwd", "c", "", "Workspace")
	c.Flags().BoolVar(&noOpen, "no-open", false, "Só mostra o endereço, sem abrir a janela")
	return c
}

func init() { rootCmd.AddCommand(newAppCommand()) }

func runApp(c *cobra.Command, cwd string, noOpen bool) error {
	c.SilenceUsage = true
	if cwd == "" {
		cwd = "."
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}
	if _, err := config.Load(root, false); err != nil {
		return err
	}
	conn, err := db.Connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := signal.NotifyContext(c.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	application, err := app.New(ctx, conn)
	if err != nil {
		return err
	}
	defer application.Shutdown()
	service, err := desktop.New(application)
	if err != nil {
		return err
	}
	listener, url, err := service.Listen()
	if err != nil {
		return err
	}
	defer listener.Close()
	// The address carries this run's key; it opens the app and nothing else.
	fmt.Fprintf(c.OutOrStdout(), "opcode app em %s\nCtrl+C encerra.\n", url)
	if noOpen || !nativeWindowAvailable {
		if !noOpen {
			openWindow(url)
		}
		return service.Serve(ctx, listener)
	}
	// The native window runs on this thread; the server answers it from another.
	served := make(chan error, 1)
	go func() { served <- service.Serve(ctx, listener) }()
	windowErr := nativeWindow(ctx, url)
	cancel() // closing the window ends the app
	if err := <-served; err != nil {
		return err
	}
	return windowErr
}

// openWindow prefers a Chromium app window (no tabs or address bar) and
// falls back to the default browser. The window gets a profile of its own:
// a browser's main profile remembers its last window size and would ignore
// the one asked for here, and the app's page shares no cookies with it.
func openWindow(url string) {
	profile := filepath.Join(os.TempDir(), "opcode-app-window")
	if dir, err := os.UserConfigDir(); err == nil {
		profile = filepath.Join(dir, "opcode", "app-window")
	}
	args := []string{"--app=" + url, "--user-data-dir=" + profile, "--window-size=1120,760", "--no-first-run", "--no-default-browser-check"}
	for _, browser := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
		if path, err := exec.LookPath(browser); err == nil {
			if exec.Command(path, args...).Start() == nil {
				return
			}
		}
	}
	_ = exec.Command("xdg-open", url).Start()
}
