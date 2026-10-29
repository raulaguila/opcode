package tui

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNotificationsAreOptInUnfocusedAndContainOnlyApplicationCopy(t *testing.T) {
	c, _ := emptySetupConfig(t)
	original := deliverNotification
	defer func() { deliverNotification = original }()
	var messages []string
	deliverNotification = func(mode string, remote bool, body string) { messages = append(messages, mode+":"+body) }
	a := appModel{unfocused: true}
	require.Nil(t, a.notify())
	c.TUI.Notifications = "native"
	a.unfocused = false
	require.Nil(t, a.notify())
	a.unfocused = true
	cmd := a.notify()
	require.NotNil(t, cmd)
	cmd()
	require.Len(t, messages, 1)
	require.Contains(t, messages[0], "attention")
	c.TUI.Notifications = "off"
	require.Nil(t, a.notify())
}
