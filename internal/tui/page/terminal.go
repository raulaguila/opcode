package page

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type TerminalTickMsg struct{}
type TerminalStartedMsg struct {
	sessionID, id string
	err           error
}

type terminalPanel struct {
	focused, starting bool
	startCancel       context.CancelFunc
	selected          string
	processes         []tools.ProcessInfo
	snapshot          tools.ProcessSnapshot
	logs              viewport.Model
}

func terminalTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return TerminalTickMsg{} })
}

func (p *chatPage) TerminalFocused() bool { return p.terminal.focused }

func (p *chatPage) resetTerminal() {
	if p.terminal.startCancel != nil {
		p.terminal.startCancel()
	}
	p.terminal = terminalPanel{}
}

func (p *chatPage) refreshTerminal() {
	if p.app == nil || p.app.Processes == nil || p.session.ID == "" {
		return
	}
	t := &p.terminal
	t.processes = p.app.Processes.List(p.session.ID)
	if len(t.processes) == 0 {
		t.selected = ""
		t.snapshot = tools.ProcessSnapshot{}
		return
	}
	found := false
	for _, process := range t.processes {
		if process.ID == t.selected {
			found = true
		}
	}
	if !found || !t.focused {
		t.selected = t.processes[len(t.processes)-1].ID
	}
	if t.focused {
		_ = p.app.Processes.Resize(p.session.ID, t.selected, max(10, p.width-2), max(3, p.height-3))
	}
	snapshot, err := p.app.Processes.Snapshot(p.session.ID, t.selected)
	if err != nil {
		return
	}
	t.snapshot = snapshot
	if t.logs.Width == 0 {
		t.logs = viewport.New(max(1, p.width-2), max(1, p.height-3))
	}
	follow := t.logs.AtBottom()
	t.logs.Width, t.logs.Height = max(1, p.width-2), max(1, p.height-3)
	t.logs.SetContent(snapshot.Stdout + snapshot.Stderr)
	if follow {
		t.logs.GotoBottom()
	}
}

func (p *chatPage) terminalVisible() bool {
	if p.terminal.selected == "" || p.height < 12 || p.width < 20 {
		return false
	}
	if p.terminal.focused {
		return true
	}
	// A bounded live preview remains visible while a command needs attention;
	// focus stays in chat so the user can steer the agent without typing in PTY.
	snapshot := p.terminal.snapshot
	return p.height >= 20 && snapshot.Running && (snapshot.PTY || snapshot.Stdout != "" || snapshot.Stderr != "")
}

func (p *chatPage) resizeContent() tea.Cmd {
	height := p.height - p.composerPanelHeight()
	if p.height >= 12 {
		height--
	}
	if !p.terminal.focused && p.terminalVisible() {
		height -= 6
	}
	if adaptive, ok := p.composer.(interface{ DesiredHeight(int, int) int }); ok {
		if split, ok := p.layout.(interface{ SetBottomHeight(int) }); ok {
			split.SetBottomHeight(adaptive.DesiredHeight(p.width, height))
		}
	}
	return p.layout.SetSize(max(0, p.width), max(0, height))
}

func (p *chatPage) toggleTerminal() tea.Cmd {
	p.terminal.focused = !p.terminal.focused
	if !p.terminal.focused {
		if p.terminal.startCancel != nil {
			p.terminal.startCancel()
			p.terminal.startCancel = nil
		}
		return p.resizeContent()
	}
	p.refreshTerminal()
	if p.terminal.selected == "" {
		return p.startTerminal()
	}
	return p.resizeContent()
}

func (p *chatPage) startTerminal() tea.Cmd {
	if p.terminal.starting {
		return nil
	}
	if p.app == nil || p.app.Processes == nil {
		return util.ReportError(fmt.Errorf("terminal manager unavailable"))
	}
	if p.app.Workflows != nil {
		mode := p.mode()
		if p.session.ID != "" {
			task, err := p.app.Workflows.Get(context.Background(), p.session.ID)
			if err != nil {
				return util.ReportError(err)
			}
			mode = task.Mode
		}
		if mode != "execute" {
			return util.ReportError(fmt.Errorf("terminal requires execute mode; use /mode execute"))
		}
	}
	var cmds []tea.Cmd
	if p.session.ID == "" {
		sess, err := p.app.Sessions.Create(context.Background(), "Terminal session")
		if err != nil {
			return util.ReportError(err)
		}
		p.session = sess
		cmds = append(cmds, p.setSidebar(), util.CmdHandler(chat.SessionSelectedMsg(sess)))
	}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, p.session.ID)
	p.terminal.startCancel = cancel
	p.terminal.starting = true
	owner := p.session.ID
	path := "/bin/bash"
	if cfg := config.Get(); cfg != nil && cfg.Shell.Path != "" {
		path = cfg.Shell.Path
	}
	command := "exec '" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
	if strings.HasSuffix(path, "/bash") || path == "bash" {
		command += " --noprofile --norc -i"
	}
	params := tools.ProcessParams{Command: command, PTY: true, Timeout: tools.MaxTimeout, Cols: max(10, p.width-2), Rows: max(3, p.height-3)}
	manager, permissions := p.app.Processes, p.app.Permissions
	cmds = append(cmds, func() tea.Msg {
		defer cancel()
		info, err := manager.Start(ctx, permissions, params)
		return TerminalStartedMsg{sessionID: owner, id: info.ID, err: err}
	})
	return tea.Batch(cmds...)
}

func (p *chatPage) terminalKey(msg tea.KeyMsg) tea.Cmd {
	t := &p.terminal
	switch msg.String() {
	case "ctrl+n":
		return p.startTerminal()
	case "alt+left", "alt+right":
		if len(t.processes) > 0 {
			index := 0
			for i, process := range t.processes {
				if process.ID == t.selected {
					index = i
					break
				}
			}
			step := 1
			if msg.String() == "alt+left" {
				step = -1
			}
			t.selected = t.processes[(index+step+len(t.processes))%len(t.processes)].ID
			p.refreshTerminal()
		}
		return nil
	case "ctrl+x":
		manager, owner, id := p.app.Processes, p.session.ID, t.selected
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := manager.Stop(ctx, owner, id); err != nil {
				return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
			}
			return TerminalTickMsg{}
		}
	}
	if !t.snapshot.PTY || !t.snapshot.Running {
		t.logs, _ = t.logs.Update(msg)
		return nil
	}
	data := terminalInput(msg)
	if t.snapshot.AppCursor && len(data) == 3 && strings.HasPrefix(data, "\x1b[") && strings.ContainsAny(data[2:], "ABCDHF") {
		data = "\x1bO" + data[2:]
	}
	if data == "" {
		return nil
	}
	if err := p.app.Processes.Input(p.session.ID, t.selected, data); err != nil {
		return util.ReportError(err)
	}
	return nil
}

func terminalInput(msg tea.KeyMsg) string {
	var data string
	switch msg.Type {
	case tea.KeyRunes:
		data = string(msg.Runes)
	case tea.KeyEnter:
		data = "\r"
	case tea.KeyBackspace:
		data = "\x7f"
	case tea.KeyUp:
		data = "\x1b[A"
	case tea.KeyDown:
		data = "\x1b[B"
	case tea.KeyRight:
		data = "\x1b[C"
	case tea.KeyLeft:
		data = "\x1b[D"
	case tea.KeyHome:
		data = "\x1b[H"
	case tea.KeyEnd:
		data = "\x1b[F"
	case tea.KeyPgUp:
		data = "\x1b[5~"
	case tea.KeyPgDown:
		data = "\x1b[6~"
	case tea.KeyDelete:
		data = "\x1b[3~"
	case tea.KeyInsert:
		data = "\x1b[2~"
	default:
		if msg.Type >= 0 && msg.Type <= 31 {
			data = string(rune(msg.Type))
		}
	}
	if msg.Alt && data != "" {
		data = "\x1b" + data
	}
	return data
}

func (p *chatPage) terminalView(full bool) string {
	t := &p.terminal
	width := max(1, p.width)
	style := styles.BaseStyle().Width(width)
	header := "Terminal • Ctrl+P back to chat"
	if t.snapshot.ID != "" {
		state := fmt.Sprintf("exit %d", t.snapshot.ExitCode)
		if t.snapshot.Running {
			state = "running"
		}
		if t.snapshot.TimedOut {
			state = "timed out"
		}
		header = fmt.Sprintf("Terminal • %s • %s", state, t.snapshot.Command)
	}
	header = ansi.Truncate(header, width, "…")
	if full {
		body := t.logs.View()
		if t.snapshot.PTY {
			body = t.snapshot.Screen
			if t.snapshot.Running && t.snapshot.CursorVisible {
				lines := strings.Split(body, "\n")
				if y := t.snapshot.CursorY; y >= 0 && y < len(lines) {
					x := t.snapshot.CursorX
					char := ansi.Cut(lines[y], x, x+1)
					if char == "" {
						char = " "
					}
					lines[y] = ansi.Cut(lines[y], 0, x) + lipgloss.NewStyle().Reverse(true).Render(char) + ansi.Cut(lines[y], x+1, width)
					body = strings.Join(lines, "\n")
				}
			}
		}
		if t.starting {
			body = "Waiting for permission to start the terminal…"
		}
		if t.selected == "" && !t.starting {
			body = "Ctrl+N starts a terminal after approval."
		}
		body = lipgloss.NewStyle().Width(width).MaxWidth(width).Height(max(1, p.height-2)).MaxHeight(max(1, p.height-2)).Render(body)
		footer := ansi.Truncate("Ctrl+P chat • Ctrl+N new • Alt+←/→ switch • Ctrl+X stop • Ctrl+C interrupt", width, "…")
		return style.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
	}
	text := strings.TrimRight(t.snapshot.Stdout+t.snapshot.Stderr, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	header = ansi.Truncate(header+" • Ctrl+P open", width, "…")
	return style.Foreground(theme.CurrentTheme().TextMuted()).Height(6).MaxHeight(6).Render(lipgloss.JoinVertical(lipgloss.Left, header, strings.Join(lines, "\n")))
}
