package fileutil

import (
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/logging"
)

var (
	rgPath  string
	fzfPath string
)

func init() {
	var err error
	rgPath, err = exec.LookPath("rg")
	if err != nil {
		logging.Warn("Ripgrep (rg) not found in $PATH. Some features might be limited or slower.")
		rgPath = ""
	}
	fzfPath, err = exec.LookPath("fzf")
	if err != nil {
		logging.Warn("FZF not found in $PATH. Some features might be limited or slower.")
		fzfPath = ""
	}
}

func GetRgCmd(globPattern string) *exec.Cmd {
	if rgPath == "" {
		return nil
	}
	rgArgs := []string{
		"--files",
		"-L",
		"--null",
	}
	if globPattern != "" {
		if !filepath.IsAbs(globPattern) && !strings.HasPrefix(globPattern, "/") {
			globPattern = "/" + globPattern
		}
		rgArgs = append(rgArgs, "--glob", globPattern)
	}
	cmd := exec.Command(rgPath, rgArgs...)
	cmd.Dir = "."
	return cmd
}

type FileInfo struct {
	Path    string
	ModTime time.Time
}

type globCollector struct {
	searchPath string
	limit      int
	matches    []FileInfo
}
