package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/spf13/cobra"
)

func newMCPCommand() *cobra.Command {
	var cwd string
	root := &cobra.Command{Use: "mcp", Short: "Autenticação explícita de integrações MCP"}
	root.PersistentFlags().StringVarP(&cwd, "cwd", "c", ".", "Workspace da configuração")
	for _, action := range []string{"login", "logout", "status"} {
		c := &cobra.Command{Use: action + " SERVER", Short: map[string]string{"login": "Autorizar MCP no navegador com PKCE", "logout": "Esquecer credenciais OAuth locais", "status": "Verificar credenciais locais sem conectar"}[action], Args: cobra.ExactArgs(1)}
		c.RunE = func(c *cobra.Command, args []string) error {
			return runMCP(c, args, action, cwd)
		}
		root.AddCommand(c)
	}
	return root
}

func runMCP(c *cobra.Command, args []string, action, cwd string) error {

	c.SilenceUsage = true
	path, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.LoadOffline(path)
	if err != nil {
		return err
	}
	server, ok := cfg.MCPServers[args[0]]
	if !ok || server.Disabled {
		return fmt.Errorf("servidor MCP desconhecido ou desativado")
	}
	switch action {
	case "login":
		if err := agent.LoginMCPOAuth(c.Context(), server, func(link string) error {
			fmt.Fprintln(c.OutOrStdout(), "Abra este endereço para autorizar o MCP (não concede permissão às ferramentas):\n"+link)
			return nil
		}); err != nil {
			return err
		}
		fmt.Fprintln(c.OutOrStdout(), "OAuth autorizado. As ferramentas continuam exigindo aprovação.")
	case "logout":
		if err := agent.LogoutMCPOAuth(c.Context(), server); err != nil {
			return err
		}
		fmt.Fprintln(c.OutOrStdout(), "Credenciais locais removidas. Para revogar o acesso remoto, use a conta do provedor.")
	case "status":
		ready, err := agent.MCPOAuthStatus(c.Context(), server)
		if err != nil {
			return err
		}
		fmt.Fprintf(c.OutOrStdout(), "Credenciais OAuth disponíveis: %t\n", ready)
	}
	return nil
}
