package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/stretchr/testify/require"
)

func TestModelSelectionRefusesBusyAgentAndDoesNotPublishOnSaveFailure(t *testing.T) {
	a, p, _, root := runtimeAgent(t, "execute")
	before := config.Get().Agents[config.AgentCoder]
	done, err := a.Run(context.Background(), "model-selection", "Hello")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	candidate := models.Model{ID: "test-discovered", APIModel: "test-new", Provider: models.ProviderOpenAI, ContextWindow: 8192, DefaultMaxTokens: 1024}
	_, err = a.SelectModel(config.AgentCoder, candidate)
	require.ErrorContains(t, err, "while processing")
	require.Equal(t, before, config.Get().Agents[config.AgentCoder])
	require.NotContains(t, models.SupportedModels, candidate.ID)
	call.reply <- provider.ProviderResponse{Content: "done"}
	require.NoError(t, finishGeneration(t, done).Error)
	// A malformed project JSON is a save failure, not a reason to switch in memory.
	require.NoError(t, os.WriteFile(filepath.Join(root, ".opcode.json"), []byte(`not-json`), 0600))
	_, err = a.SelectModel(config.AgentCoder, candidate)
	require.Error(t, err)
	require.Equal(t, p, a.provider)
	require.Equal(t, before, config.Get().Agents[config.AgentCoder])
	require.NotContains(t, models.SupportedModels, candidate.ID)
}
