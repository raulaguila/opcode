package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
)

type BashParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
	PTY     bool   `json:"pty,omitempty"`
}

type BashPermissionsParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
	PTY     bool   `json:"pty,omitempty"`
}

type BashResponseMetadata struct {
	ProcessID string `json:"process_id,omitempty"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	ExitCode  int    `json:"exit_code"`
	TimedOut  bool   `json:"timed_out"`
	Cancelled bool   `json:"cancelled"`
	Truncated bool   `json:"truncated"`
}
type bashTool struct {
	permissions permission.Service
	manager     *ProcessManager
	ownsManager bool
}

const (
	BashToolName = "bash"

	DefaultTimeout  = 1 * 60 * 1000  // 1 minutes in milliseconds
	MaxTimeout      = 10 * 60 * 1000 // 10 minutes in milliseconds
	MaxOutputLength = 30000
)

var bannedCommands = []string{
	"alias", "curl", "curlie", "wget", "axel", "aria2c",
	"nc", "telnet", "lynx", "w3m", "links", "httpie", "xh",
	"http-prompt", "chrome", "firefox", "safari",
}

func bashDescription() string {
	return `Execute an authorized command in an independent process rooted in the workspace. Shell state does not persist between calls. Commands use the user-global security policy: filtered environment, bounded output and, by default on Linux, a Bubblewrap sandbox with no network and credential paths masked. Failure to start the sandbox is an error. Every execution needs permission unless explicitly approved for the session.
Use grep/glob/view for file exploration, typed Git tools for inspection, run_checks for configured verification commands, and process_start/status/stop for background work. Run related shell setup and commands in one invocation. Results include exit_code, timed_out, cancelled and truncated metadata; nonzero exits and interruptions are errors. Timeout is in milliseconds (default 60000, maximum 600000).`
}

func NewBashTool(permission permission.Service, managers ...*ProcessManager) BaseTool {
	var manager *ProcessManager
	owned := len(managers) == 0 || managers[0] == nil
	if owned {
		manager = NewProcessManager()
	} else {
		manager = managers[0]
	}
	return &bashTool{
		permissions: permission,
		manager:     manager,
		ownsManager: owned,
	}
}

func (b *bashTool) Info() ToolInfo {
	return ToolInfo{
		Name:        BashToolName,
		Description: bashDescription(),
		Parameters: map[string]any{
			"pty": map[string]any{"type": "boolean", "description": "Allocate a PTY for interactive commands. The user can enter input in the terminal panel. Use process_start for work that should return immediately."},
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Optional timeout in milliseconds (max 600000)",
			},
		},
		Required: []string{"command"},
	}
}

func (b *bashTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params BashParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("invalid parameters"), nil
	}

	if params.Timeout > MaxTimeout {
		params.Timeout = MaxTimeout
	} else if params.Timeout <= 0 {
		params.Timeout = DefaultTimeout
	}

	if strings.TrimSpace(params.Command) == "" {
		return NewTextErrorResponse("missing command"), nil
	}

	baseCmd := strings.Fields(params.Command)[0]
	for _, banned := range bannedCommands {
		if strings.EqualFold(baseCmd, banned) {
			return NewTextErrorResponse(fmt.Sprintf("command '%s' is not allowed", baseCmd)), nil
		}
	}

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}
	p := b.permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        config.WorkingDirectory(),
		ToolName:    BashToolName,
		Action:      "execute",
		Description: fmt.Sprintf("Execute command: %s", params.Command),
		Params: BashPermissionsParams{
			Command: params.Command,
			Timeout: params.Timeout,
			PTY:     params.PTY,
		},
	})
	if !p {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}
	startTime := time.Now()
	record, err := b.manager.startProcess(ctx, sessionID, ProcessParams{Command: params.Command, Timeout: params.Timeout, PTY: params.PTY}, false)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("error executing command: %w", err)
	}
	<-record.done
	stdout, _ := record.stdout.Snapshot()
	stderr, _ := record.stderr.Snapshot()
	exitCode, interrupted := record.exit, record.timedOut || ctx.Err() != nil

	artifactNote := ""
	if len(stdout)+len(stderr) > MaxOutputLength {
		if snapshot, snapshotErr := SnapshotOutput(ctx, "bash", stdout+"\n"+stderr); snapshotErr == nil {
			artifactNote = "\n[Command output saved as artifact]\n" + snapshot
		}
	}
	stdout = truncateOutput(stdout)
	stderr = truncateOutput(stderr)

	errorMessage := stderr
	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	} else if exitCode != 0 {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Exit code %d", exitCode)
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}

	metadata := BashResponseMetadata{
		ProcessID: record.id,
		StartTime: startTime.UnixMilli(),
		EndTime:   time.Now().UnixMilli(),
		ExitCode:  exitCode, TimedOut: interrupted && ctx.Err() == nil, Cancelled: ctx.Err() != nil, Truncated: strings.Contains(stdout, "output truncated") || strings.Contains(stderr, "output truncated"),
	}
	if stdout == "" {
		return WithResponseMetadata(NewTextResponse("no output"), metadata), nil
	}
	if artifactNote != "" {
		stdout = artifactNote + "\n" + stdout
	}
	response := NewTextResponse(stdout)
	response.IsError = exitCode != 0 || interrupted
	return WithResponseMetadata(response, metadata), nil
}

// Only the creator owns shutdown. App-provided managers also serve the PTY and
// checks and must survive rebuilding an idle provider agent.
func (b *bashTool) Close() error {
	if b.ownsManager {
		return b.manager.Close()
	}
	return nil
}
func (b *bashTool) CancelSession(session string) { b.manager.CancelSession(session) }

func truncateOutput(content string) string {
	if len(content) <= MaxOutputLength {
		return content
	}

	halfLength := MaxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]

	truncatedLinesCount := countLines(content[halfLength : len(content)-halfLength])
	return fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", start, truncatedLinesCount, end)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}
