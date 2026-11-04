package tools

import (
	"context"
	"time"
)

func (m *ProcessManager) start(parent context.Context, owner, command string, lifetime time.Duration) (*managedProcess, error) {
	return m.startProcess(parent, owner, ProcessParams{Command: command, Timeout: int(lifetime.Milliseconds())}, true)
}

func IsTestSource(path string) bool { return projectFileRole(path) == "test" }

func codeChunks(path, text string) []indexedChunk {
	return codeChunksContext(context.Background(), path, text)
}

func planIndexRefresh(root string, incremental bool) indexRefreshPlan {
	return planIndexRefreshLimit(root, incremental, 0)
}
