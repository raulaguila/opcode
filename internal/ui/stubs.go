package ui

import tea "charm.land/bubbletea/v2"

func (m *Model) openSettings() tea.Cmd { return nil }
func (m *Model) togglePlan() tea.Cmd   { m.showPlan = !m.showPlan; m.layout(); return nil }
