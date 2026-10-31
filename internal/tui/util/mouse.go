package util

import tea "charm.land/bubbletea/v2"

// TranslateMouse keeps the native event kind while moving into a child view.
func TranslateMouse(msg tea.MouseMsg, dx, dy int) tea.MouseMsg {
	m := msg.Mouse()
	m.X, m.Y = m.X+dx, m.Y+dy
	switch msg.(type) {
	case tea.MouseClickMsg:
		return tea.MouseClickMsg(m)
	case tea.MouseReleaseMsg:
		return tea.MouseReleaseMsg(m)
	case tea.MouseMotionMsg:
		return tea.MouseMotionMsg(m)
	case tea.MouseWheelMsg:
		return tea.MouseWheelMsg(m)
	default:
		return msg
	}
}

func MousePressed(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseClickMsg)
	return ok
}

func MouseReleased(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseReleaseMsg)
	return ok
}

func MouseMoved(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseMotionMsg)
	return ok
}
