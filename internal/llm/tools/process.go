package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/permission"
)

type ProcessParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	PTY     bool   `json:"pty,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

type managedProcess struct {
	id, owner, command string
	cmd                *exec.Cmd
	cancel             context.CancelFunc
	done               chan struct{}
	stdout, stderr     *shell.Buffer
	exit               int
	started            time.Time
	finished, timedOut bool
	pty                *os.File
	input              chan []byte
	screen             vt10x.Terminal
	screenMu           sync.Mutex
	partialRune        []byte
	cols, rows         int
}

type ProcessManager struct {
	mu        sync.Mutex
	processes map[string]*managedProcess
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewProcessManager() *ProcessManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProcessManager{processes: map[string]*managedProcess{}, ctx: ctx, cancel: cancel}
}

// Start is the permission boundary shared by the human UI and agent tools.
func (m *ProcessManager) Start(ctx context.Context, permissions permission.Service, params ProcessParams) (ProcessInfo, error) {
	permissionCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	defer cancel()
	ctx = permissionCtx
	owner, _ := GetContextValues(ctx)
	if owner == "" || strings.TrimSpace(params.Command) == "" {
		return ProcessInfo{}, fmt.Errorf("session and command are required")
	}
	if redact(params.Command) != params.Command {
		return ProcessInfo{}, fmt.Errorf("command contains a configured credential")
	}
	if permissions == nil || !permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID: owner, ToolName: "process_start", Action: "execute", Path: config.WorkingDirectory(),
		Description: "Start command", Params: params,
	}) {
		return ProcessInfo{}, permission.ErrorPermissionDenied
	}
	process, err := m.startProcess(ctx, owner, params, true)
	if err != nil {
		return ProcessInfo{}, err
	}
	return m.info(process), nil
}

func (m *ProcessManager) start(parent context.Context, owner, command string, lifetime time.Duration) (*managedProcess, error) {
	return m.startProcess(parent, owner, ProcessParams{Command: command, Timeout: int(lifetime.Milliseconds())}, true)
}

func (m *ProcessManager) startProcess(parent context.Context, owner string, params ProcessParams, detached bool) (*managedProcess, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if params.Timeout <= 0 {
		params.Timeout = DefaultTimeout
	}
	params.Timeout = min(params.Timeout, MaxTimeout)
	params.Cols, params.Rows = terminalSize(params.Cols, params.Rows)
	base := parent
	if detached {
		base = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(base, time.Duration(params.Timeout)*time.Millisecond)
	command := shell.Command
	if params.PTY {
		command = shell.PTYCommand
	}
	cmd, err := command(ctx, config.WorkingDirectory(), params.Command)
	if err != nil {
		cancel()
		return nil, err
	}
	record := &managedProcess{
		id: uuid.NewString(), owner: owner, command: params.Command, cmd: cmd, cancel: cancel,
		done: make(chan struct{}), stdout: &shell.Buffer{}, stderr: &shell.Buffer{},
		started: time.Now(), exit: -1, cols: params.Cols, rows: params.Rows,
	}
	m.mu.Lock()
	running := 0
	for id, process := range m.processes {
		if !process.finished {
			running++
		} else if len(m.processes) >= 32 {
			delete(m.processes, id)
		}
	}
	if m.closed || running >= 4 || len(m.processes) >= 32 {
		m.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("process manager closed or process limit reached")
	}
	readDone := make(chan struct{})
	if params.PTY {
		record.screen = vt10x.New(vt10x.WithSize(params.Cols, params.Rows))
		record.input = make(chan []byte, 32)
		record.pty, err = shell.StartPTY(cmd, params.Cols, params.Rows)
		if err == nil {
			go func() {
				defer close(readDone)
				io.Copy(terminalOutput{record}, record.pty)
			}()
			go func() {
				for {
					select {
					case data := <-record.input:
						if _, err := record.pty.Write(data); err != nil {
							return
						}
					case <-ctx.Done():
						return
					}
				}
			}()
		}
	} else {
		cmd.Stdout, cmd.Stderr = record.stdout, record.stderr
		err = cmd.Start()
		close(readDone)
	}
	if err != nil {
		m.mu.Unlock()
		cancel()
		return nil, err
	}
	m.processes[record.id] = record
	m.mu.Unlock()
	go func() {
		cmd.Wait()
		// Descendants may still hold the slave; bound draining before closing
		// the master so cancellation releases readers and input writers.
		select {
		case <-readDone:
		case <-time.After(500 * time.Millisecond):
		}
		if record.pty != nil {
			record.pty.Close()
		}
		<-readDone
		m.mu.Lock()
		record.finished = true
		if cmd.ProcessState != nil {
			record.exit = cmd.ProcessState.ExitCode()
		}
		record.timedOut = ctx.Err() == context.DeadlineExceeded
		m.mu.Unlock()
		cancel()
		close(record.done)
	}()
	return record, nil
}

type terminalOutput struct{ process *managedProcess }

func (w terminalOutput) Write(data []byte) (int, error) {
	p := w.process
	p.stdout.Write(data)
	p.screenMu.Lock()
	defer p.screenMu.Unlock()
	combined := append(p.partialRune, data...)
	n, _ := p.screen.Write(combined)
	p.partialRune = append(p.partialRune[:0], combined[n:]...)
	return len(data), nil
}

func terminalSize(cols, rows int) (int, int) {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	return max(10, min(cols, 240)), max(3, min(rows, 80))
}

func (m *ProcessManager) lookup(owner, id string) (*managedProcess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.processes[id]
	if owner == "" || record == nil || record.owner != owner {
		return nil, fmt.Errorf("process not found in this session")
	}
	return record, nil
}

// Input queues keystrokes in order without blocking the UI on a terminal write.
// Agent input must pass process_write's separate permission check first.
func (m *ProcessManager) Input(owner, id, data string) error {
	record, err := m.lookup(owner, id)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 16384 {
		return fmt.Errorf("terminal input must contain 1 to 16384 bytes")
	}
	if record.pty == nil {
		return fmt.Errorf("process has no interactive PTY")
	}
	select {
	case <-record.done:
		return fmt.Errorf("process has exited")
	default:
	}
	select {
	case record.input <- []byte(data):
		return nil
	default:
		return fmt.Errorf("terminal input queue is full; wait before sending more input")
	}
}

func (m *ProcessManager) Resize(owner, id string, cols, rows int) error {
	record, err := m.lookup(owner, id)
	if err != nil || record.pty == nil {
		return err
	}
	cols, rows = terminalSize(cols, rows)
	record.screenMu.Lock()
	defer record.screenMu.Unlock()
	if record.cols == cols && record.rows == rows {
		return nil
	}
	if err := shell.ResizePTY(record.pty, cols, rows); err != nil {
		return err
	}
	record.cols, record.rows = cols, rows
	record.screen.Resize(cols, rows)
	return nil
}

func (m *ProcessManager) Stop(ctx context.Context, owner, id string) error {
	record, err := m.lookup(owner, id)
	if err != nil {
		return err
	}
	record.cancel()
	select {
	case <-record.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *ProcessManager) CancelSession(session string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, process := range m.processes {
		if process.owner == session {
			process.cancel()
		}
	}
}

func (m *ProcessManager) Close() error {
	m.cancel()
	m.mu.Lock()
	m.closed = true
	records := make([]*managedProcess, 0, len(m.processes))
	for _, record := range m.processes {
		record.cancel()
		records = append(records, record)
	}
	m.mu.Unlock()
	for _, record := range records {
		<-record.done
	}
	return nil
}
