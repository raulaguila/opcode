package tools

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
)

// ProcessInfo contains only bounded, sanitized data safe to send to the UI.
type ProcessInfo struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Command   string    `json:"command"`
	PTY       bool      `json:"pty"`
	Running   bool      `json:"running"`
	ExitCode  int       `json:"exit_code"`
	TimedOut  bool      `json:"timed_out"`
	StartedAt time.Time `json:"started_at"`
}

type ProcessSnapshot struct {
	ProcessInfo
	Stdout        string `json:"stdout"`
	Stderr        string `json:"stderr"`
	Screen        string `json:"screen,omitempty"`
	CursorX       int    `json:"cursor_x,omitempty"`
	CursorY       int    `json:"cursor_y,omitempty"`
	CursorVisible bool   `json:"cursor_visible,omitempty"`
	AppCursor     bool   `json:"app_cursor,omitempty"`
	Truncated     bool   `json:"truncated"`
}

func (m *ProcessManager) info(record *managedProcess) ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ProcessInfo{ID: record.id, SessionID: record.owner, Command: cleanProcessOutput(redact(record.command)),
		PTY: record.pty != nil, Running: !record.finished, ExitCode: record.exit, TimedOut: record.timedOut, StartedAt: record.started}
}

func (m *ProcessManager) List(owner string) []ProcessInfo {
	m.mu.Lock()
	var records []*managedProcess
	for _, record := range m.processes {
		if record.owner == owner {
			records = append(records, record)
		}
	}
	m.mu.Unlock()
	result := make([]ProcessInfo, 0, len(records))
	for _, record := range records {
		result = append(result, m.info(record))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartedAt.Before(result[j].StartedAt) })
	return result
}

func (m *ProcessManager) Snapshot(owner, id string) (ProcessSnapshot, error) {
	record, err := m.lookup(owner, id)
	if err != nil {
		return ProcessSnapshot{}, err
	}
	info := m.info(record)
	out, cutOut := record.stdout.Snapshot()
	stderr, cutErr := record.stderr.Snapshot()
	screen := ""
	var cursorX, cursorY int
	var cursorVisible, appCursor bool
	if record.screen != nil {
		record.screenMu.Lock()
		screen = record.screen.String()
		cursor := record.screen.Cursor()
		cursorX, cursorY = cursor.X, cursor.Y
		cursorVisible = record.screen.CursorVisible()
		appCursor = record.screen.Mode()&vt10x.ModeAppCursor != 0
		record.screenMu.Unlock()
	}
	return ProcessSnapshot{ProcessInfo: info,
		Stdout: redactLive(cleanProcessOutput(out), info.Running), Stderr: redactLive(cleanProcessOutput(stderr), info.Running),
		Screen: cleanProcessOutput(redactScreen(screen, info.Running)), Truncated: cutOut || cutErr,
		CursorX: cursorX, CursorY: cursorY, CursorVisible: cursorVisible, AppCursor: appCursor}, nil
}

// Subprocess control sequences must never reach the user's host terminal.
func cleanProcessOutput(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(text))
}

func (m *ProcessManager) snapshot(record *managedProcess) ToolResponse {
	if record == nil {
		return NewTextErrorResponse("process record expired")
	}
	snapshot, err := m.Snapshot(record.owner, record.id)
	if err != nil {
		return NewTextErrorResponse(err.Error())
	}
	response := jsonToolResult(snapshot)
	response.IsError = !snapshot.Running && (snapshot.ExitCode != 0 || snapshot.TimedOut)
	return response
}
