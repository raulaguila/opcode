package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/server"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	var address, cwd string
	c := &cobra.Command{Use: "serve", Short: "Servidor local autenticado para sessões e terminais reconectáveis", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		return runServe(c, address, cwd)
	}}
	c.Flags().StringVar(&address, "listen", "127.0.0.1:4096", "IP loopback e porta")
	c.Flags().StringVarP(&cwd, "cwd", "c", "", "Workspace servido")
	return c
}

func init() { rootCmd.AddCommand(newServeCommand(), newAttachCommand(), newMCPCommand()) }

func runServe(c *cobra.Command, address, cwd string) error {

	c.SilenceUsage = true
	token := os.Getenv("OPCODE_SERVER_TOKEN")
	if len(token) < 32 {
		return fmt.Errorf("defina OPCODE_SERVER_TOKEN com pelo menos 32 caracteres; ele nunca é impresso")
	}
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
	service, err := server.New(ctx, application, conn, token)
	if err != nil {
		return err
	}
	defer service.Close()
	listener, err := server.ListenLocal(address)
	if err != nil {
		return err
	}
	defer listener.Close()
	fmt.Fprintf(c.OutOrStdout(), "Opcode em http://%s · workspace %s\nReconecte com opcode attach --url http://%s\n", listener.Addr(), root, listener.Addr())
	return service.Serve(listener)
}
