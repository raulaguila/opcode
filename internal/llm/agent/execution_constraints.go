package agent

import (
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/permission"
)

// sandboxBroken is sent even without command tools loaded: the model would
// otherwise find out by failing, and could claim checks it never ran.
const sandboxBroken = "Host state: the command sandbox cannot start on this machine, so bash, run_checks and tests are refused. Do not call them. Read, search and analyse instead, and say in your answer that nothing was executed. The user can see the cause and the fix with /sandbox; never try to change the sandbox yourself.\n"

// commandsRefused ignores a session on full access, which runs no sandbox.
func (a *agent) commandsRefused(session string) bool {
	return shell.SandboxProblem() != nil && !permission.FullAccess(a.permissions, session)
}

// Only policy metadata belongs in the prompt, never environment values or paths.
func (a *agent) executionConstraints(run *generationRun, session string) map[string]any {
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
	broken := a.commandsRefused(session)
	if broken {
		sandbox = "unavailable"
	}
	return map[string]any{
		"contract":                             contract,
		"bash_allowed":                         !broken && requestedToolAvailable(run, "bash"),
		"run_checks_allowed":                   !broken && requestedToolAvailable(run, "run_checks"),
		"sandbox":                              sandbox,
		"sandbox_network_enabled":              network,
		"host_network_without_sandbox":         sandbox == "none",
		"command_network_can_request_approval": true,
		"go_cache":                             "private per-project cache reused across commands",
		"default_command_timeout_ms":           tools.DefaultTimeout,
		"max_command_timeout_ms":               tools.MaxTimeout,
		"environment":                          "filtered; HOME is empty and temporary; shell setup does not persist between calls",
	}
}
