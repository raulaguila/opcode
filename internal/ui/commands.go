package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type commandEntry struct {
	id, processID, command, output, state string
	since                                 int64
	exit                                  *int
	truncated                             bool
}

func commandText(call message.ToolCall) string {
	if call.Name != "bash" && call.Name != "process_start" {
		return ""
	}
	var input struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(call.Input), &input) != nil {
		return ""
	}
	return safeCommandText(input.Command)
}

func safeCommandText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(tools.RedactCredentials(text)))
}

func (m *Model) pollProcesses() {
	var snapshots []tools.ProcessSnapshot
	if m.app != nil && m.app.Processes != nil && m.session.ID != "" {
		for _, info := range m.app.Processes.List(m.session.ID) {
			if snapshot, err := m.app.Processes.Snapshot(m.session.ID, info.ID); err == nil {
				snapshots = append(snapshots, snapshot)
			}
		}
	}
	if !slices.Equal(m.processes, snapshots) {
		m.processes = snapshots
		m.processVersion++
	}
}

// Commands are projected from stored calls/results; live snapshots only supplement them.
func (m *Model) commandEntries() []commandEntry {
	results := map[string]message.ToolResult{}
	for _, msg := range m.messages {
		for _, result := range msg.ToolResults() {
			results[result.ToolCallID] = result
		}
	}
	var entries []commandEntry
	for _, msg := range m.messages {
		if msg.IsApplication() || msg.Role != message.Assistant {
			continue
		}
		for _, call := range msg.ToolCalls() {
			command := commandText(call)
			if command == "" {
				continue
			}
			entry := commandEntry{id: call.ID, command: command, state: "running", since: msg.CreatedAt}
			if finish := msg.FinishPart(); finish != nil {
				entry.since = finish.Time
			}
			if result, ok := results[call.ID]; ok {
				entry.applyResult(call.Name, result)
			}
			entries = append(entries, entry)
		}
	}
	for _, snapshot := range m.processes {
		if snapshot.SessionID != m.session.ID {
			continue
		}
		matched := matchingCommand(entries, snapshot, m.processes)
		if matched >= 0 {
			applySnapshot(&entries[matched], snapshot)
		} else {
			entry := commandEntry{id: "process:" + snapshot.ID, command: snapshot.Command}
			applySnapshot(&entry, snapshot)
			entries = append(entries, entry)
		}
	}
	return entries
}

func (entry *commandEntry) applyResult(name string, result message.ToolResult) {
	entry.state, entry.output = "ok", safeCommandText(result.Content)
	if result.IsError {
		entry.state = "failed"
	}
	var meta struct {
		ProcessID string `json:"process_id"`
		ExitCode  *int   `json:"exit_code"`
		Truncated bool   `json:"truncated"`
		Cancelled bool   `json:"cancelled"`
		TimedOut  bool   `json:"timed_out"`
	}
	if json.Unmarshal([]byte(result.Metadata), &meta) == nil {
		entry.processID, entry.exit, entry.truncated = meta.ProcessID, meta.ExitCode, meta.Truncated
		if meta.Cancelled {
			entry.state = "cancelled"
		} else if meta.TimedOut {
			entry.state = "timed out"
		}
	}
	if name != "process_start" {
		return
	}
	var snapshot tools.ProcessSnapshot
	if json.Unmarshal([]byte(result.Content), &snapshot) != nil || snapshot.ID == "" {
		return
	}
	entry.output = ""
	applySnapshot(entry, snapshot)
	if snapshot.Running {
		entry.state = "unknown"
	}
}

func matchingCommand(entries []commandEntry, snapshot tools.ProcessSnapshot, processes []tools.ProcessSnapshot) int {
	for i := range entries {
		if entries[i].processID == snapshot.ID {
			return i
		}
	}
	// Before bash returns its process ID, match only an unambiguous live command.
	matched := -1
	for i, entry := range entries {
		if entry.state != "running" || entry.processID != "" || entry.command != snapshot.Command || entry.since > snapshot.StartedAt.Unix() {
			continue
		}
		if matched >= 0 {
			return -1
		}
		matched = i
	}
	if matched < 0 {
		return -1
	}
	count := 0
	for _, other := range processes {
		if other.SessionID == snapshot.SessionID && other.Command == snapshot.Command && other.StartedAt.Unix() >= entries[matched].since {
			count++
		}
	}
	if count != 1 {
		return -1
	}
	return matched
}

func applySnapshot(entry *commandEntry, snapshot tools.ProcessSnapshot) {
	entry.processID, entry.since = snapshot.ID, snapshot.StartedAt.Unix()
	entry.truncated = entry.truncated || snapshot.Truncated
	// Keep the persisted result (including artifact references) after bash finishes.
	if entry.state == "running" || entry.state == "unknown" || entry.output == "" {
		entry.output = snapshot.Stdout
		if snapshot.Stderr != "" {
			entry.output += "\n" + textFor("stderr:", "stderr:") + "\n" + snapshot.Stderr
		}
	}
	if snapshot.Running {
		entry.state, entry.exit = "running", nil
		return
	}
	entry.exit = new(int)
	*entry.exit = snapshot.ExitCode
	if entry.state != "cancelled" && entry.state != "timed out" {
		entry.state = "ok"
		if snapshot.ExitCode != 0 {
			entry.state = "failed"
		}
		if snapshot.TimedOut {
			entry.state = "timed out"
		}
	}
}

func (entry commandEntry) status() string {
	switch entry.state {
	case "running":
		if entry.since <= 0 {
			return textFor("executando", "running")
		}
		return fmt.Sprintf(textFor("executando · %ds", "running · %ds"), max(0, time.Now().Unix()-entry.since))
	case "cancelled":
		return textFor("cancelado", "cancelled")
	case "timed out":
		return textFor("tempo esgotado", "timed out")
	case "unknown":
		return textFor("estado atual indisponível", "current status unavailable")
	}
	if entry.exit != nil {
		return fmt.Sprintf(textFor("saída %d", "exit %d"), *entry.exit)
	}
	if entry.state == "failed" {
		return textFor("falhou", "failed")
	}
	return textFor("concluído", "completed")
}

func (m *Model) runningProcesses() bool {
	return slices.ContainsFunc(m.processes, func(p tools.ProcessSnapshot) bool { return p.SessionID == m.session.ID && p.Running })
}

func commandRows(entry commandEntry, width int, waiting bool) []string {
	icon, color := "✓", P.Success
	if entry.state == "running" {
		icon, color = "●", P.Plan
	} else if entry.state == "unknown" {
		icon, color = "?", P.Muted
	} else if entry.state != "ok" {
		icon, color = "✗", P.Danger
	}
	status := entry.status()
	if waiting && entry.state == "running" && entry.processID == "" {
		status = textFor("aguardando aprovação", "waiting for approval")
	}
	rows := []string{on(P.Ground, P.Muted).Render("› ") + on(P.Ground, color).Render(icon) + on(P.Ground, P.Muted).Render(" bash · "+status)}
	for _, line := range strings.Split(ansi.Hardwrap(strings.ReplaceAll(entry.command, "\t", "    "), max(1, width-4), true), "\n") {
		rows = append(rows, on(P.Ground, P.Strong).Render("    "+line))
	}
	if entry.state != "ok" && strings.TrimSpace(entry.output) != "" {
		lines := strings.Split(ansi.Hardwrap(strings.ReplaceAll(strings.TrimRight(entry.output, "\n"), "\t", "    "), max(1, width-4), true), "\n")
		for _, line := range lines[max(0, len(lines)-6):] {
			rows = append(rows, on(P.Ground, P.Muted).Render("    "+line))
		}
	}
	rows = append(rows, on(P.Ground, P.Faint).Render("    "+textFor("Ctrl+P · detalhes e copiar", "Ctrl+P · details and copy")))
	return rows
}

type commandDialog struct {
	id     string
	offset int
	notice string
}

func (m *Model) openCommands() tea.Cmd {
	m.clearSelection()
	m.pollProcesses()
	entries := m.commandEntries()
	if len(entries) == 0 {
		m.info = textFor("Nenhum comando nesta conversa", "No commands in this conversation")
		return nil
	}
	m.dialog = &commandDialog{id: entries[len(entries)-1].id}
	return nil
}

func (d *commandDialog) current(m *Model) ([]commandEntry, int) {
	entries := m.commandEntries()
	for i := range entries {
		if entries[i].id == d.id {
			return entries, i
		}
	}
	return entries, max(0, len(entries)-1)
}

func (d *commandDialog) view(m *Model) string {
	entries, index := d.current(m)
	box := Box{Title: textFor("Comandos", "Commands"), Width: min(100, m.width-4), PadX: 2, PadY: 1}
	if len(entries) == 0 {
		return box.Render(textFor("Nenhum comando", "No commands"))
	}
	entry := entries[index]
	box.Meta = fmt.Sprintf("%d/%d · %s", index+1, len(entries), entry.status())
	box.Legend = textFor("←/→ · ↑/↓ · C copiar · O saída · Ctrl+X parar · Esc", "←/→ · ↑/↓ · C copy · O output · Ctrl+X stop · Esc")
	if d.notice != "" {
		box.Legend = d.notice
	}
	text := "$ " + entry.command + "\n\n" + textFor("Saída capturada:", "Captured output:") + "\n" + entry.output
	if strings.TrimSpace(entry.output) == "" {
		text += textFor("(sem saída)", "(no output)")
	}
	if entry.truncated {
		text += "\n" + textFor("[Saída parcial; limite de captura atingido]", "[Partial output; capture limit reached]")
	}
	lines := strings.Split(ansi.Hardwrap(strings.ReplaceAll(text, "\t", "    "), box.Inner(), true), "\n")
	height := max(1, m.height-12)
	d.offset = min(max(0, d.offset), max(0, len(lines)-height))
	var rows []string
	for _, line := range lines[d.offset:min(len(lines), d.offset+height)] {
		rows = append(rows, on(P.Surface, P.Text).Render(line))
	}
	return box.Render(strings.Join(rows, "\n"))
}

func (d *commandDialog) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	d.notice = ""
	entries, index := d.current(m)
	if msg.String() == "esc" {
		m.dialog = nil
		return nil, true
	}
	if len(entries) == 0 {
		return nil, true
	}
	entry := entries[index]
	switch msg.String() {
	case "left":
		d.id = entries[max(0, index-1)].id
		d.offset = 0
	case "right":
		d.id = entries[min(len(entries)-1, index+1)].id
		d.offset = 0
	case "up":
		d.offset = max(0, d.offset-1)
	case "down":
		d.offset++
	case "pgup":
		d.offset = max(0, d.offset-max(1, m.height-12))
	case "pgdown":
		d.offset += max(1, m.height-12)
	case "home":
		d.offset = 0
	case "end":
		d.offset = int(^uint(0) >> 1)
	case "c", "ctrl+c":
		return copyChatText(entry.command), true
	case "o":
		return copyChatText(entry.output), true
	case "ctrl+x":
		if entry.state == "running" && entry.processID != "" && m.app != nil && m.app.Processes != nil {
			manager, owner, id := m.app.Processes, m.session.ID, entry.processID
			return func() tea.Msg { return commandStoppedMsg{owner, manager.Stop(context.Background(), owner, id)} }, true
		}
	}
	return nil, true
}

type commandStoppedMsg struct {
	session string
	err     error
}
