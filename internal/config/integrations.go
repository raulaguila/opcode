package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"
	"unicode"
)

// Integration is a validated settings candidate, never an executable tool call.
type Integration struct {
	Kind, Name string
	MCP        MCPServer
	LSP        LSPConfig
}

func ValidateIntegration(candidate Integration) error {
	if err := validateProfileID(candidate.Name); err != nil {
		return fmt.Errorf("integration name: %w", err)
	}
	switch candidate.Kind {
	case "mcp":
		s := candidate.MCP
		if err := s.ValidateOAuth(); err != nil {
			return err
		}
		if len(s.Args) > 64 || len(s.Env) > 64 || len(s.Headers) > 64 || len(s.HeadersEnv) > 64 || len(s.EnvVars) > 64 {
			return fmt.Errorf("integration settings exceed 64 entries")
		}
		switch s.Type {
		case "", MCPStdio:
			if strings.TrimSpace(s.Command) == "" && !s.Disabled {
				return fmt.Errorf("stdio MCP requires a command")
			}
		case MCPHTTP, MCPSse:
			u, err := url.Parse(s.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return fmt.Errorf("MCP URL must be HTTP(S), without credentials, query or fragment")
			}
		default:
			return fmt.Errorf("MCP type must be stdio, http or sse")
		}
		if len(s.Command) > 4096 || strings.ContainsRune(s.Command, 0) {
			return fmt.Errorf("invalid MCP command")
		}
		for _, arg := range s.Args {
			if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
				return fmt.Errorf("invalid MCP argument")
			}
		}
		for name, value := range s.Headers {
			if !validHeaderName(name) || strings.ContainsAny(value, "\r\n\x00") {
				return fmt.Errorf("invalid MCP header")
			}
		}
		for name, env := range s.HeadersEnv {
			if !validHeaderName(name) || !validEnvironmentName(env) {
				return fmt.Errorf("headersEnv requires valid header names and environment variable names")
			}
		}
		for name, env := range s.EnvVars {
			if !validEnvironmentName(name) || !validEnvironmentName(env) {
				return fmt.Errorf("envVars requires valid environment variable names")
			}
		}
	case "lsp":
		s := candidate.LSP
		if err := s.ValidateRouting(); err != nil {
			return err
		}
		if len(s.Command) > 4096 || strings.ContainsRune(s.Command, 0) || strings.TrimSpace(s.Command) == "" && !s.Disabled {
			return fmt.Errorf("LSP requires a valid command")
		}
		if len(s.Args) > 64 {
			return fmt.Errorf("LSP allows at most 64 arguments")
		}
		for _, arg := range s.Args {
			if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
				return fmt.Errorf("invalid LSP argument")
			}
		}
	default:
		return fmt.Errorf("integration kind must be mcp or lsp")
	}
	return nil
}
func validHeaderName(name string) bool {
	if len(name) == 0 || len(name) > 256 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return true
}

// ResolveMCPServer copies the raw definition before resolving secrets. Callers
// must never persist the returned value. No key appears in URLs or errors.
func ResolveMCPServer(raw MCPServer) (MCPServer, error) {
	if err := ValidateIntegration(Integration{Kind: "mcp", Name: "connection", MCP: raw}); err != nil {
		return MCPServer{}, err
	}
	if raw.Disabled {
		return MCPServer{}, fmt.Errorf("MCP integration is disabled")
	}
	result := raw
	result.Args = slices.Clone(raw.Args)
	result.Env = slices.Clone(raw.Env)
	result.Headers = maps.Clone(raw.Headers)
	if result.Headers == nil {
		result.Headers = map[string]string{}
	}
	for header, name := range raw.HeadersEnv {
		secret, ok := os.LookupEnv(name)
		if !ok || secret == "" {
			return MCPServer{}, fmt.Errorf("MCP header environment variable %s is not set", name)
		}
		if strings.ContainsAny(secret, "\r\n\x00") {
			return MCPServer{}, fmt.Errorf("MCP header environment value is invalid")
		}
		for existing := range result.Headers {
			if strings.EqualFold(existing, header) {
				delete(result.Headers, existing)
			}
		}
		result.Headers[header] = secret
	}
	for target, name := range raw.EnvVars {
		secret, ok := os.LookupEnv(name)
		if !ok {
			return MCPServer{}, fmt.Errorf("MCP environment variable %s is not set", name)
		}
		if strings.ContainsRune(secret, 0) {
			return MCPServer{}, fmt.Errorf("MCP environment value is invalid")
		}
		filtered := result.Env[:0]
		for _, item := range result.Env {
			key, _, _ := strings.Cut(item, "=")
			if key != target {
				filtered = append(filtered, item)
			}
		}
		result.Env = filtered
		result.Env = append(result.Env, target+"="+secret)
	}
	return result, nil
}
func SaveIntegration(candidate Integration) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if err := ValidateIntegration(candidate); err != nil {
		return err
	}
	// Keep caller-owned form maps out of the published credential registry.
	if candidate.Kind == "mcp" {
		candidate.MCP.Args = slices.Clone(candidate.MCP.Args)
		candidate.MCP.Env = slices.Clone(candidate.MCP.Env)
		candidate.MCP.Headers = maps.Clone(candidate.MCP.Headers)
		candidate.MCP.HeadersEnv = maps.Clone(candidate.MCP.HeadersEnv)
		candidate.MCP.EnvVars = maps.Clone(candidate.MCP.EnvVars)
	}
	update := func(file *Config) {
		if candidate.Kind == "mcp" {
			if file.MCPServers == nil {
				file.MCPServers = map[string]MCPServer{}
			}
			file.MCPServers[candidate.Name] = candidate.MCP
		} else {
			if file.LSP == nil {
				file.LSP = map[string]LSPConfig{}
			}
			file.LSP[candidate.Name] = candidate.LSP
		}
	}
	path := ProjectConfigPath(cfg.WorkingDir)
	data, err := os.ReadFile(path)
	project := false
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot read project configuration: %w", err)
	}
	if err == nil {
		var local Config
		if json.Unmarshal(data, &local) != nil {
			return fmt.Errorf("invalid project configuration JSON")
		}
		if candidate.Kind == "mcp" {
			_, project = local.MCPServers[candidate.Name]
		} else {
			_, project = local.LSP[candidate.Name]
		}
	}
	if project {
		err = patchConfigFile(path, data, update)
	} else {
		err = updateCfgFile(update)
	}
	if err != nil {
		return err
	}
	credentialMu.Lock()
	defer credentialMu.Unlock()
	if candidate.Kind == "mcp" {
		if cfg.MCPServers == nil {
			cfg.MCPServers = map[string]MCPServer{}
		}
		cfg.MCPServers[candidate.Name] = candidate.MCP
	} else {
		if cfg.LSP == nil {
			cfg.LSP = map[string]LSPConfig{}
		}
		cfg.LSP[candidate.Name] = candidate.LSP
	}
	return nil
}

// IntegrationSummary is deliberately limited to non-sensitive endpoint metadata.
func IntegrationSummary(candidate Integration) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, s)
	}
	if candidate.Kind == "lsp" {
		state := "enabled"
		if candidate.LSP.Disabled {
			state = "disabled"
		}
		return clean(candidate.Name + " · LSP · " + state)
	}
	state := "enabled"
	if candidate.MCP.Disabled {
		state = "disabled"
	}
	kind := string(candidate.MCP.Type)
	if kind == "" {
		kind = "stdio"
	}
	return clean(candidate.Name + " · MCP " + kind + " · " + state)
}
