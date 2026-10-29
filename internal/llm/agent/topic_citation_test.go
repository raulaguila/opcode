package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestProjectExplanationCannotCiteOnlyImplementationManifest(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Execute)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# Project\nFeatures are described here.\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/project\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	done, err := a.Run(context.Background(), "s", "o que pode me falar do projeto atual?")
	require.NoError(t, err)
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "As funcionalidades estão em go.mod:1.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "topic source")
	repair.reply <- provider.ProviderResponse{Content: "As funcionalidades estão descritas em README.md:1-2.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}
