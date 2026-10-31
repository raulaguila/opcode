package page

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type TerminalTickMsg struct{}
type scheduledTerminalTickMsg struct{}
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
	resizedID         string
	cols, rows        int
}

func (p *chatPage) terminalTick() tea.Cmd {
	interval := time.Second
	if p.liveActivity() {
		interval = 100 * time.Millisecond
	}
	return tea.Tick(interval, func(time.Time) tea.Msg { return scheduledTerminalTickMsg{} })
}
func (p *chatPage) liveActivity() bool {
	if p.terminal.focused || p.terminal.starting || p.terminal.snapshot.Running || p.workflow.question.ID != "" || p.progress != "" {
		return true
	}
	return p.app != nil && p.app.CoderAgent != nil && p.app.CoderAgent.IsSessionBusy(p.session.ID)
}
func (p *chatPage) pollTerminal(manual bool) tea.Cmd {
	before := p.terminalVisible()
	panelHeight := p.composerPanelHeight()
	if manual {
		p.refreshWorkflow()
	} else {
		p.pollWorkflow()
	}
	p.refreshTerminal()
	var resize tea.Cmd
	if before != p.terminalVisible() || panelHeight != p.composerPanelHeight() {
		resize = p.resizeContent()
	}
	return tea.Batch(resize, p.terminalTick())
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
	dimensionsChanged := p.resizeTerminal()
	if t.snapshot.ID == t.selected && !t.snapshot.Running && !dimensionsChanged {
		return
	}
	snapshot, err := p.app.Processes.Snapshot(p.session.ID, t.selected)
	if err != nil {
		return
	}
	p.installTerminalSnapshot(snapshot)
}
func (p *chatPage) resizeTerminal() bool {
	t := &p.terminal
	cols, rows := max(10, p.width-2), p.terminalRows()
	if !t.focused || (t.resizedID == t.selected && t.cols == cols && t.rows == rows) {
		return false
	}
	if p.app.Processes.Resize(p.session.ID, t.selected, cols, rows) != nil {
		return false
	}
	t.resizedID, t.cols, t.rows = t.selected, cols, rows
	return true
}

func (p *chatPage) terminalRows() int {
	rows := p.height - 3 // title, divider and controls
	if len(p.terminal.processes) > 1 {
		rows--
	}
	return max(3, rows)
}
func (p *chatPage) installTerminalSnapshot(snapshot tools.ProcessSnapshot) {
	t := &p.terminal
	changed := t.snapshot.ID != snapshot.ID || t.snapshot.Stdout != snapshot.Stdout || t.snapshot.Stderr != snapshot.Stderr
	t.snapshot = snapshot
	if t.logs.Width() == 0 {
		t.logs = viewport.New(viewport.WithWidth(max(1, p.width-2)), viewport.WithHeight(p.terminalRows()))
		changed = true
	}
	follow := t.logs.AtBottom()
	t.logs.SetWidth(max(1, p.width-2))
	t.logs.SetHeight(p.terminalRows())
	if changed {
		t.logs.SetContent(snapshot.Stdout + snapshot.Stderr)
	}
	if changed && follow {
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
	if result0, handled := startTerminalAppWorkflows(p); handled {
		return result0
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
	params := tools.ProcessParams{Command: command, PTY: true, Timeout: tools.MaxTimeout, Cols: max(10, p.width-2), Rows: p.terminalRows()}
	manager, permissions := p.app.Processes, p.app.Permissions
	cmds = append(cmds, func() tea.Msg {
		defer cancel()
		info, err := manager.Start(ctx, permissions, params)
		return TerminalStartedMsg{sessionID: owner, id: info.ID, err: err}
	})
	return tea.Batch(cmds...)
}

func (p *chatPage) terminalKey(msg tea.KeyPressMsg) tea.Cmd {
	t := &p.terminal
	switch msg.String() {
	case "ctrl+n":
		return p.startTerminal()
	case "alt+left", "alt+right":
		return terminalKeyLeftAltRight(p, &msg, t)
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

func terminalInput(msg tea.KeyPressMsg) string {
	data := terminalKeySequence(msg.Code)
	if data == "" {
		data = msg.Text
		if data == "" && msg.Code >= 32 && msg.Code <= 0x10ffff {
			data = string(msg.Code)
		}
	}
	if msg.Mod&tea.ModCtrl != 0 {
		if control := terminalControlInput(msg.Code); control != "" {
			data = control
		}
	}
	if msg.Code == tea.KeyTab && msg.Mod&tea.ModShift != 0 {
		data = "\x1b[Z"
	}
	if msg.Mod&tea.ModAlt != 0 && data != "" {
		data = "\x1b" + data
	}
	return data
}

func terminalControlInput(code rune) string {
	if code >= 'a' && code <= 'z' {
		code -= 'a' - 'A'
	}
	if code == ' ' {
		return "\x00"
	}
	if code >= '@' && code <= '_' {
		return string(code & 0x1f)
	}
	return ""
}

func terminalKeySequence(code rune) string {
	switch code {
	case tea.KeyEnter:
		return "\r"
	case tea.KeyTab:
		return "\t"
	case tea.KeyEscape:
		return "\x1b"
	case tea.KeyBackspace:
		return "\x7f"
	case tea.KeyUp:
		return "\x1b[A"
	case tea.KeyDown:
		return "\x1b[B"
	case tea.KeyRight:
		return "\x1b[C"
	case tea.KeyLeft:
		return "\x1b[D"
	case tea.KeyHome:
		return "\x1b[H"
	case tea.KeyEnd:
		return "\x1b[F"
	case tea.KeyPgUp:
		return "\x1b[5~"
	case tea.KeyPgDown:
		return "\x1b[6~"
	case tea.KeyDelete:
		return "\x1b[3~"
	case tea.KeyInsert:
		return "\x1b[2~"
	}
	return ""
}

func (p *chatPage) terminalView(full bool) string {
	t := &p.terminal
	width := max(1, p.width)
	style := styles.BaseStyle().Width(width)
	header := "Terminal"
	if t.snapshot.ID != "" {
		state := fmt.Sprintf("exit %d", t.snapshot.ExitCode)
		if t.snapshot.Running {
			state = config.Text("em execução", "running")
		}
		if t.snapshot.TimedOut {
			state = config.Text("tempo esgotado", "timed out")
		}
		header = fmt.Sprintf("Terminal • %s • %s", state, t.snapshot.Command)
	}
	header = ansi.Truncate(header, width, "…")
	if result0, handled := terminalViewFull(p, full, t, width, &style, &header); handled {
		return result0
	}
	text := strings.TrimRight(t.snapshot.Stdout+t.snapshot.Stderr, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	header = ansi.Truncate(header+styles.PanelHint().Render(config.Text(" • Ctrl+P abre", " • Ctrl+P open")), width, "…")
	return style.Foreground(theme.CurrentTheme().TextMuted()).Height(6).MaxHeight(6).Render(lipgloss.JoinVertical(lipgloss.Left, header, strings.Join(lines, "\n")))
}

func terminalViewFull(p *chatPage, full bool, t *terminalPanel, width int, style *lipgloss.Style, header *string) (extractedResult0 string, handled bool) {
	if full {
		(*header) = ansi.Truncate((*header)+" · "+config.Text("Entrada no terminal", "Terminal input"), width, "…")
		if len(t.processes) > 1 {
			labels := []string{}
			for _, process := range t.processes {
				mark := " "
				if process.ID == t.selected {
					mark = "›"
				}
				labels = append(labels, mark+process.ID)
			}
			(*header) += "\n" + ansi.Truncate(strings.Join(labels, " · "), width, "…")
		}
		body := t.logs.View()
		terminalViewFullTSnapshotPTY(t, width, &body)
		if t.starting {
			body = config.Text("Aguardando aprovação para iniciar o terminal…", "Waiting for permission to start the terminal…")
		}
		if t.selected == "" && !t.starting {
			body = config.Text("Ctrl+N inicia um terminal após aprovação.", "Ctrl+N starts a terminal after approval.")
		}
		body = lipgloss.NewStyle().Width(width).MaxWidth(width).Height(max(1, p.height-2-lipgloss.Height((*header)))).MaxHeight(max(1, p.height-2-lipgloss.Height((*header)))).Render(body)
		footer := ansi.Truncate(config.Text("Ctrl+P conversa • Ctrl+N novo • Alt+←/→ troca • Ctrl+X encerra • Ctrl+C interrompe", "Ctrl+P chat • Ctrl+N new • Alt+←/→ switch • Ctrl+X stop • Ctrl+C interrupt"), width, "…")
		return style.Render(lipgloss.JoinVertical(lipgloss.Left, styles.PanelTitle().Width(width).Render((*header)), body, styles.PanelDivider(width), styles.PanelHint().Render(footer))), true
	}
	return extractedResult0, false
}

func terminalViewFullTSnapshotPTY(t *terminalPanel, width int, body *string) {
	if t.snapshot.PTY {
		(*body) = t.snapshot.Screen
		if t.snapshot.Running && t.snapshot.CursorVisible {
			lines := strings.Split((*body), "\n")
			if y := t.snapshot.CursorY; y >= 0 && y < len(lines) {
				x := t.snapshot.CursorX
				char := ansi.Cut(lines[y], x, x+1)
				if char == "" {
					char = " "
				}
				lines[y] = ansi.Cut(lines[y], 0, x) + lipgloss.NewStyle().Reverse(true).Render(char) + ansi.Cut(lines[y], x+1, width)
				(*body) = strings.Join(lines, "\n")
			}
		}
	}
}

func terminalKeyLeftAltRight(p *chatPage, msg *tea.KeyPressMsg, t *terminalPanel) (extractedResult0 tea.Cmd) {
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

}

func startTerminalAppWorkflows(p *chatPage) (extractedResult0 tea.Cmd, handled bool) {
	if p.app.Workflows != nil {
		mode := p.mode()
		if p.session.ID != "" {
			task, err := p.app.Workflows.Get(context.Background(), p.session.ID)
			if err != nil {
				return util.ReportError(err), true
			}
			mode = task.Mode
		}
		if mode != "execute" {
			return util.ReportError(fmt.Errorf("terminal requires execute mode; use /mode execute")), true
		}
	}
	return extractedResult0, false
}
