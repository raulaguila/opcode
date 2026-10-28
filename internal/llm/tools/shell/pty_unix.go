//go:build !windows

package shell

import (
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"

	"github.com/creack/pty"
)

func StartPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	// setsid and setpgid cannot both run in the child. StartWithSize creates
	// the session and controlling terminal; its process group ID is its PID.
	if cmd.SysProcAttr != nil {
		cmd.SysProcAttr.Setpgid = false
	}
	var master atomic.Pointer[os.File]
	previousCancel := cmd.Cancel
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killPTYSession(cmd.Process.Pid)
		}
		var err error
		if previousCancel != nil {
			err = previousCancel()
		} else if cmd.Process != nil {
			err = cmd.Process.Kill()
		}
		if terminal := master.Load(); terminal != nil {
			_ = terminal.Close()
		}
		return err
	}
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	if err := syscall.SetNonblock(int(terminal.Fd()), true); err != nil {
		terminal.Close()
		_ = cmd.Cancel()
		_ = cmd.Wait()
		return nil, err
	}
	master.Store(terminal)
	return terminal, nil
}

func ResizePTY(terminal *os.File, cols, rows int) error {
	return pty.Setsize(terminal, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
