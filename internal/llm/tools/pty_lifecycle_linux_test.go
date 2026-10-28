package tools

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestPTYCancellationStopsInteractiveBackgroundJobs(t *testing.T) {
	testUnisolated(t)
	m := NewProcessManager()
	defer m.Close()
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	info, err := m.Start(toolContext(), p, ProcessParams{Command: "exec /bin/bash --noprofile --norc -i", PTY: true, Timeout: 5000})
	require.NoError(t, err)
	require.NoError(t, m.Input("test-session", info.ID, "sleep 60 & printf 'CHILD_PID:%s\\n' \"$!\"\n"))
	var child string
	require.Eventually(t, func() bool {
		s, err := m.Snapshot("test-session", info.ID)
		if err != nil {
			return false
		}
		match := regexp.MustCompile(`CHILD_PID:(\d+)`).FindStringSubmatch(s.Stdout)
		if len(match) < 2 {
			return false
		}
		child = match[1]
		return true
	}, time.Second, 10*time.Millisecond)
	m.CancelSession("test-session")
	awaitProcess(t, m, info.ID, func(s ProcessSnapshot) bool { return !s.Running })
	require.Eventually(t, func() bool {
		data, err := os.ReadFile("/proc/" + child + "/stat")
		if os.IsNotExist(err) {
			return true
		}
		if err != nil {
			return false
		}
		at := strings.LastIndexByte(string(data), ')')
		if at < 0 {
			return false
		}
		fields := strings.Fields(string(data)[at+1:])
		return len(fields) > 0 && fields[0] == "Z"
	}, time.Second, 10*time.Millisecond, "background job survived terminal cancellation")
}
