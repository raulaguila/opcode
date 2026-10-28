package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBorrowedProcessManagersSurviveClosingTools(t *testing.T) {
	shared := NewProcessManager()
	defer shared.Close()
	borrowed := []BaseTool{NewBashTool(nil, shared), NewChecksTool(nil, shared)}
	borrowed = append(borrowed, NewProcessTools(shared, nil)...)
	for _, tool := range borrowed {
		require.NoError(t, tool.(interface{ Close() error }).Close())
		shared.mu.Lock()
		closed := shared.closed
		shared.mu.Unlock()
		require.False(t, closed, "closing borrowed %s must leave the application manager alive", tool.Info().Name)
	}
}
func TestStandaloneToolsCloseTheirOwnedProcessManagers(t *testing.T) {
	bash := NewBashTool(nil).(*bashTool)
	check := NewChecksTool(nil).(*checksTool)
	require.NoError(t, bash.Close())
	require.NoError(t, check.Close())
	require.True(t, bash.manager.closed)
	require.True(t, check.manager.closed)
}
