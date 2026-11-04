package dialog

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
)

func readIntegrationFields(c *config.Integration, value func(int) string) error {
	if c.Kind == "mcp" {
		return readMCPFields(c, value)
	}
	c.LSP.Command = value(1)
	if json.Unmarshal([]byte(value(2)), &c.LSP.Args) != nil {
		return fmt.Errorf("arguments must be a JSON array of strings")
	}
	var err error
	c.LSP.Disabled, err = strconv.ParseBool(value(3))
	if err != nil {
		return fmt.Errorf("disabled must be true or false")
	}
	return nil
}
func readMCPFields(c *config.Integration, value func(int) string) error {
	c.MCP.Type = config.MCPType(value(1))
	c.MCP.Command = value(2)
	c.MCP.URL = value(4)
	if json.Unmarshal([]byte(value(3)), &c.MCP.Args) != nil {
		return fmt.Errorf("arguments must be a JSON array of strings")
	}
	if json.Unmarshal([]byte(value(5)), &c.MCP.HeadersEnv) != nil {
		return fmt.Errorf("headers must be a JSON object mapping header to environment name")
	}
	if json.Unmarshal([]byte(value(6)), &c.MCP.EnvVars) != nil {
		return fmt.Errorf("environment must be a JSON object mapping target to source variable")
	}
	var err error
	c.MCP.Disabled, err = strconv.ParseBool(value(7))
	if err != nil {
		return fmt.Errorf("disabled must be true or false")
	}
	return readMCPOAuthFields(c, value)
}
func readMCPOAuthFields(c *config.Integration, value func(int) string) error {
	enabled, err := strconv.ParseBool(value(8))
	if err != nil {
		return fmt.Errorf("OAuth enabled must be true or false")
	}
	c.MCP.OAuth = nil
	if !enabled {
		return nil
	}
	port, err := strconv.Atoi(value(12))
	if err != nil {
		return fmt.Errorf("OAuth redirect port must be an integer")
	}
	c.MCP.OAuth = &config.MCPOAuthConfig{ClientID: value(9), ClientSecretEnv: value(10), Scopes: strings.Fields(value(11)), RedirectPort: port}
	return nil
}

func integrationJSON(value any) string {
	raw, _ := json.Marshal(value)
	if string(raw) != "null" {
		return string(raw)
	}
	if _, ok := value.([]string); ok {
		return "[]"
	}
	return "{}"
}
