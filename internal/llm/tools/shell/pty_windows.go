//go:build windows

package shell

import (
	"fmt"
	"os"
	"os/exec"
)

func StartPTY(*exec.Cmd, int, int) (*os.File, error) {
	return nil, fmt.Errorf("interactive PTYs are currently supported on Unix only")
}

func ResizePTY(*os.File, int, int) error {
	return fmt.Errorf("interactive PTYs are currently supported on Unix only")
}
