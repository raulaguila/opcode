package dialog

import tea "github.com/charmbracelet/bubbletea"

// A handler either owns an event (including a no-op) or leaves it to the
// focused text input. Dialogs retain their model identity across navigation.
type connectionAction struct {
	command tea.Cmd
	handled bool
}

func connectionHandled(command tea.Cmd) connectionAction {
	return connectionAction{command: command, handled: true}
}
