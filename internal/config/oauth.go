package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// MCPOAuthConfig contains references and public settings only. Tokens and
// dynamically registered client secrets live in the user's private auth store.
type MCPOAuthConfig struct {
	ClientID        string   `json:"clientID,omitempty"`
	ClientSecretEnv string   `json:"clientSecretEnv,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
	RedirectPort    int      `json:"redirectPort,omitempty"`
}

func AuthDirectory() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "opcode", "auth"), nil
}

func (s MCPServer) ValidateOAuth() error {
	if s.OAuth == nil {
		return nil
	}
	if s.Type != MCPHTTP && s.Type != MCPSse {
		return fmt.Errorf("OAuth requires an HTTP or SSE MCP server")
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid OAuth resource URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && net.ParseIP(u.Hostname()).IsLoopback()) {
		return fmt.Errorf("MCP OAuth requires HTTPS (HTTP is allowed only for a loopback IP)")
	}
	o := s.OAuth
	if len(o.ClientID) > 2048 || strings.ContainsAny(o.ClientID, "\r\n\x00") || o.RedirectPort < 0 || o.RedirectPort > 65535 || len(o.Scopes) > 32 {
		return fmt.Errorf("invalid MCP OAuth settings")
	}
	if o.ClientSecretEnv != "" && !validEnvironmentName(o.ClientSecretEnv) {
		return fmt.Errorf("OAuth clientSecretEnv requires an environment variable name")
	}
	return validateOAuthScopesAndHeaders(s)
}

func validateOAuthScopesAndHeaders(s MCPServer) error {
	for _, scope := range s.OAuth.Scopes {
		if scope == "" || len(scope) > 256 || strings.ContainsAny(scope, " \r\n\x00\t") {
			return fmt.Errorf("invalid OAuth scope")
		}
	}
	for name := range s.Headers {
		if strings.EqualFold(name, "Authorization") {
			return fmt.Errorf("OAuth cannot be combined with an Authorization header")
		}
	}
	for name := range s.HeadersEnv {
		if strings.EqualFold(name, "Authorization") {
			return fmt.Errorf("OAuth cannot be combined with an Authorization header")
		}
	}
	return nil
}
