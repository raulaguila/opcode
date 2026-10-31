package chat

import (
	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/message"
)

const transcriptPageTurns = 64

type historyNavigation uint8

const (
	historyNavigationNone historyNavigation = iota
	historySearch
	historyKeyboardSelection
	historyMouseSelection
)

type transcriptHistory struct {
	first            string
	initial          bool
	before           *message.HistoryCursor
	more, paged, all bool
	command          tea.Cmd
	resume           func()
	navigation       historyNavigation
}

func (m *messagesCmp) resetTranscriptHistory() { m.history = transcriptHistory{} }

// PendingHistoryCommand preserves the optional navigation interface used by
// page controls and mocks while allowing persistence reads to run as commands.
func PendingHistoryCommand(control any) tea.Cmd {
	if control, ok := control.(interface{ HistoryCommand() tea.Cmd }); ok {
		return control.HistoryCommand()
	}
	return nil
}

func (m *messagesCmp) HistoryCommand() tea.Cmd {
	command := m.history.command
	m.history.command = nil
	return command
}

func (m *messagesCmp) deferHistory(navigation historyNavigation, resume func()) bool {
	if !m.rendering && !m.history.more {
		return false
	}
	m.history.all, m.history.resume = true, resume
	m.history.navigation = navigation
	if !m.rendering && m.history.command == nil {
		m.history.command = m.requestHistoryRead(true)
	}
	return true
}

// Drop only the requested navigation. An in-flight read and its integrity
// reconciliation still finish; closing a panel must not cancel that snapshot.
func (m *messagesCmp) cancelPendingHistoryNavigation(navigation historyNavigation) bool {
	if m.history.navigation != navigation {
		return false
	}
	m.history.all, m.history.resume, m.history.navigation = false, nil, historyNavigationNone
	return true
}

func (m *messagesCmp) continueHistoryRead() tea.Cmd {
	if !m.history.all {
		return nil
	}
	if m.history.more {
		return m.requestHistoryRead(true)
	}
	m.history.all, m.history.navigation = false, historyNavigationNone
	resume := m.history.resume
	m.history.resume = nil
	if resume != nil {
		resume()
	}
	return m.HistoryCommand()
}

// Older persistence pages remain available. Search and selection finish loading
// history before recording transcript coordinates.
func (m *messagesCmp) visibleRenderGroups() []turnGroup {
	groups := m.renderGroups()
	if m.history.initial && !m.reconcile.needed {
		m.history.initial = false
		if len(groups) > transcriptPageTurns && !m.find.open {
			m.history.first = groups[len(groups)-transcriptPageTurns].key
		}
	}
	if m.history.first == "" {
		return groups
	}
	start, ok := m.renderCache.groupIndex[m.history.first]
	if !ok {
		m.history.first = ""
		return groups
	}
	return groups[start:]
}

func (m *messagesCmp) loadEarlierHistory(all bool) {
	if m.rendering {
		return
	}
	if m.history.more && (all || m.history.first == "") {
		m.history.all = m.history.all || all
		if m.history.command == nil {
			m.history.command = m.requestHistoryRead(true)
		}
	}
	if m.history.first == "" {
		return
	}
	groups := m.renderGroups()
	start := max(0, m.renderCache.groupIndex[m.history.first]-transcriptPageTurns)
	if all {
		start = 0
	}
	m.history.first = ""
	if start > 0 {
		m.history.first = groups[start].key
	}
	offset, lines := m.viewport.YOffset, len(m.transcriptLines)
	m.renderCache.dirty = true
	m.renderView()
	m.viewport.SetYOffset(offset + len(m.transcriptLines) - lines)
}

func (m *messagesCmp) updateTranscriptViewport(msg tea.Msg) tea.Cmd {
	delta := m.viewport.scrollDelta(msg)
	if delta < 0 && m.viewport.YOffset+delta < 0 {
		m.loadEarlierHistory(false)
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return tea.Batch(cmd, m.HistoryCommand())
}
