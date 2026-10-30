//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package localstate

import (
	"fmt"
	"os"
)

func tryFileLock(*os.File) (bool, error) {
	return false, fmt.Errorf("cross-process state locking is unavailable on this platform")
}
