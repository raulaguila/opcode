package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExtensionsRequireExplicitBoundedConfiguration(t *testing.T) {
	require.NoError(t, (ExtensionsConfig{}).Validate())
	require.NoError(t, (ExtensionsConfig{Skills: []string{"/home/user/skills"}, Hooks: []HookConfig{{Event: "before_tool", Tool: "write", Command: "go vet ./...", TimeoutSeconds: 30}}}).Validate())
	for _, cfg := range []ExtensionsConfig{{Skills: []string{"relative"}}, {Hooks: []HookConfig{{Event: "before_tool", Command: "true", TimeoutSeconds: 31}}}, {Hooks: []HookConfig{{Event: "startup", Command: "true"}}}, {Hooks: []HookConfig{{Event: "after_tool", Command: " "}}}} {
		require.Error(t, cfg.Validate())
	}
}
