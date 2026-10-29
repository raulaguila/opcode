package util

import tea "github.com/charmbracelet/bubbletea"

// CopyTextMsg is emitted only by explicit UI copy actions. Result is optional;
// a selection keeps its highlight regardless of clipboard availability.
type CopyTextMsg struct {
	Text   string
	Result func(confirmed bool, err error) tea.Msg
}
