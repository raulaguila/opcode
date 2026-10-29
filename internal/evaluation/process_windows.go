package evaluation

import (
	"os/exec"
	"time"
)

func configureEvaluationProcess(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
