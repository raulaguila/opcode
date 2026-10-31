package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/raulaguila/opcode/internal/config"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Notifications contain fixed application copy only, never prompts, paths,
// tool arguments, credentials, or model output. They confer no permissions.
func (a appModel) notify() tea.Cmd {
	if !a.unfocused || config.Get() == nil {
		return nil
	}
	mode := config.Get().TUI.Notifications
	if mode == "" || mode == "off" {
		return nil
	}
	remote := os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
	body := config.Text("A atividade terminou ou precisa da sua atenção.", "Activity finished or needs your attention.")
	return func() tea.Msg { deliverNotification(mode, remote, body); return nil }
}

var deliverNotification = func(mode string, remote bool, body string) {
	if mode == "auto" {
		if remote {
			mode = "bell"
		} else {
			mode = "native"
		}
	}
	if mode == "native" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var command *exec.Cmd
		switch runtime.GOOS {
		case "linux":
			command = exec.CommandContext(ctx, "notify-send", "--app-name=Opcode", "Opcode", body)
		case "darwin":
			command = exec.CommandContext(ctx, "osascript", "-e", `display notification "Activity finished or needs your attention." with title "Opcode"`)
		}
		if command != nil && command.Run() == nil {
			return
		}
		mode = "bell"
	}
	if mode == "osc" {
		_, _ = io.WriteString(os.Stderr, "\x1b]777;notify;Opcode;"+body+"\x07")
		return
	}
	if mode == "bell" {
		_, _ = io.WriteString(os.Stderr, "\a")
	}
}
