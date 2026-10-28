package shell

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Interactive shells put jobs in separate process groups. All of those groups
// still belong to the private session created for this PTY. Kill its children
// before its leader so background jobs cannot keep the slave open on shutdown.
func killPTYSession(leader int) {
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == leader {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(data)[end+1:])
		if len(fields) < 4 {
			continue
		}
		session, err := strconv.Atoi(fields[3])
		if err == nil && session == leader {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}
