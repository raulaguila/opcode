package agent

import (
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
)

// Only policy metadata belongs in the prompt, never environment values or paths.
func executionConstraints(run *generationRun) map[string]any {
	contract := ""
	if run.contract != nil {
		contract = run.contract.Kind
	}
	sandbox, network := "bubblewrap", false
	if cfg := config.Get(); cfg != nil {
		if cfg.Security.Sandbox != "" {
			sandbox = cfg.Security.Sandbox
		}
		network = cfg.Security.AllowNetwork
	}
	return map[string]any{
		"contract":                             contract,
		"bash_allowed":                         requestedToolAvailable(run, "bash"),
		"run_checks_allowed":                   requestedToolAvailable(run, "run_checks"),
		"sandbox":                              sandbox,
		"sandbox_network_enabled":              network,
		"host_network_without_sandbox":         sandbox == "none",
		"command_network_can_request_approval": true,
		"go_cache":                             "private per-project cache reused across commands",
		"default_command_timeout_ms":           tools.DefaultTimeout,
		"max_command_timeout_ms":               tools.MaxTimeout,
		"environment":                          "filtered; HOME disabled; shell setup does not persist between calls",
	}
}
