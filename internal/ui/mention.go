package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/fileutil"
)

const mentionRows = 6

// mentionToken is the @word the composer ends with, without the @.
func (m *Model) mentionToken() (string, bool) {
	value := m.composer.Value()
	start := strings.LastIndexAny(value, " \t\n") + 1
	if !strings.HasPrefix(value[start:], "@") {
		return "", false
	}
	return value[start+1:], true
}

// filesMsg carries the workspace's files, listed off the interface's loop.
type filesMsg []string

// findMention lists the files matching the @ being typed, if any. The
// workspace is listed in the background (a large one takes a while) and
// kept a few seconds; until it arrives the last listing is used.
func (m *Model) findMention() tea.Cmd {
	query, ok := m.mentionToken()
	if !ok {
		m.mention = nil
		return nil
	}
	m.mention = fileutil.MatchFiles(m.files, query, mentionRows)
	m.mentionPick = min(m.mentionPick, max(0, len(m.mention)-1))
	if m.listing || time.Since(m.filesAt) < 10*time.Second {
		return nil
	}
	m.listing = true
	root := config.WorkingDirectory()
	return func() tea.Msg { return filesMsg(fileutil.WorkspaceFiles(root)) }
}

// mentionKey moves through the list, inserts the chosen file or closes it.
func (m *Model) mentionKey(key string) bool {
	switch key {
	case "up":
		m.mentionPick = (m.mentionPick + len(m.mention) - 1) % len(m.mention)
	case "down":
		m.mentionPick = (m.mentionPick + 1) % len(m.mention)
	case "tab", "enter":
		query, _ := m.mentionToken()
		value := m.composer.Value()
		m.composer.SetValue(value[:len(value)-len(query)] + m.mention[m.mentionPick] + " ")
		m.mention, m.mentionPick = nil, 0
		m.layout()
	case "esc":
		m.mention, m.mentionPick = nil, 0
		m.layout()
	default:
		return false
	}
	return true
}

// mentionView is the list of files over the composer, the chosen one marked.
func (m *Model) mentionView() string {
	rows := make([]string, len(m.mention))
	for i, path := range m.mention {
		row := on(P.Surface, P.Text).Render("    @" + path)
		if i == m.mentionPick {
			row = on(P.Surface, P.Ask).Bold(true).Render("  ▸ @" + path)
		}
		rows[i] = Line(row, m.width, P.Surface)
	}
	return strings.Join(rows, "\n")
}
