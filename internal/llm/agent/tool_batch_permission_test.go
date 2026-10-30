package agent

import (
	"context"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDeniedToolStopsRemainingBatch(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	denied := &hookDenyTool{name: "view"}
	next := &countedTool{name: "ls", output: "must not execute"}
	a.tools = []tools.BaseTool{denied, next}
	done, err := a.Run(context.Background(), "s", "Leia os recursos disponíveis")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{}`), toolCall("ls", `{}`))
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.EqualValues(t, 1, denied.count.Load())
	require.Zero(t, next.count.Load(), "denial must stop the batch, not only leave the select statement")
}
