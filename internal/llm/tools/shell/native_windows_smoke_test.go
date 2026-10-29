//go:build windows

package shell

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeSmokeWindowsPTYRefused(t *testing.T) {
	terminal, err := StartPTY(exec.Command("must-not-start.exe"), 80, 24)
	require.Nil(t, terminal)
	require.ErrorContains(t, err, "Unix only")
	require.ErrorContains(t, ResizePTY(nil, 80, 24), "Unix only")
}
