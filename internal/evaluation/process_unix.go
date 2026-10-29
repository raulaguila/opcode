//go:build !windows

package evaluation

import (
	"os/exec"
	"syscall"
	"time"
)

// Match the shell runner's cancellation policy: a timed-out adapter/worker
// must not leave its ordinary subprocesses running or hold output pipes open.
func configureEvaluationProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
}
