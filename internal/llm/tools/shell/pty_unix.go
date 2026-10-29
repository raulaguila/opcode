//go:build !windows

package shell

import (
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
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
	terminal, err = pollablePTYMaster(terminal)
	if err != nil {
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

// pollablePTYMaster transfers ownership of a fresh master into a Go-polled file.
// Darwin's pty.Open uses os.NewFile on a blocking descriptor. Setting O_NONBLOCK
// afterwards does not register that wrapper with the runtime poller: Read would
// return EAGAIN instead of waiting. Duplicate atomically with CLOEXEC, retire the
// old wrapper, and set the flag before NewFile detects it. No readers may have
// started on original when this function is called.
func pollablePTYMaster(original *os.File) (*os.File, error) {
	if original == nil {
		return nil, fmt.Errorf("PTY master is unavailable")
	}
	name := original.Name()
	descriptor, err := unix.FcntlInt(original.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	_ = original.Close()
	if err != nil {
		return nil, fmt.Errorf("duplicate PTY master: %w", err)
	}
	if err := unix.SetNonblock(descriptor, true); err != nil {
		_ = unix.Close(descriptor)
		return nil, fmt.Errorf("set PTY master nonblocking: %w", err)
	}
	terminal := os.NewFile(uintptr(descriptor), name)
	if terminal == nil {
		_ = unix.Close(descriptor)
		return nil, fmt.Errorf("create pollable PTY master")
	}
	// A missing OS polling capability must fail clearly, never silently lose
	// terminal output. Zero clears the deadline and adds no runtime I/O timeout.
	if err := terminal.SetReadDeadline(time.Time{}); err != nil {
		_ = terminal.Close()
		return nil, fmt.Errorf("PTY master polling unavailable: %w", err)
	}
	return terminal, nil
}
