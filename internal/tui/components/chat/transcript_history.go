package chat

import tea "github.com/charmbracelet/bubbletea"

const transcriptPageTurns = 64

type transcriptHistory struct {
	first   string
	initial bool
}

// Durable messages remain complete. Only initial visual work is paged; search
// and selection materialize all historical rows before recording coordinates.
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
	if m.history.first == "" || m.rendering {
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
	return cmd
}
