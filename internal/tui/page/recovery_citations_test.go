package page

import (
	"errors"
	"fmt"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/stretchr/testify/require"
)

func TestCitationFailureNeverBecomesContextLimit(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("failed to process events: %w: include the source supplied by context preparation", agent.ErrSourceCitation),
		errors.New("source citation remains missing after repair; read evidence preserved: context preparation"),
	} {
		require.Equal(t, config.Text("Citações não validadas", "Citations not validated"), recoveryTitle(err))
		require.NotContains(t, recoveryActions(err), "/context")
		require.Contains(t, recoveryActions(err), "/error")
	}
	require.Equal(t, config.Text("Execução interrompida", "Run interrupted"), recoveryTitle(errors.New("cannot load context preferences")))
	require.Equal(t, config.Text("Limite do modelo atingido", "Model limit reached"), recoveryTitle(fmt.Errorf("generation: %w", agent.ErrContextLimit)))
}

func TestOutputFailureHasSeparateTitleAndSettingsAction(t *testing.T) {
	p, _ := workspacePage(t)
	for _, err := range []error{fmt.Errorf("generation: %w", provider.ErrOutputLimit), errors.New("model output token limit reached; incomplete call rejected")} {
		require.Equal(t, config.Text("Limite de saída atingido", "Output limit reached"), recoveryTitle(err))
		require.Contains(t, recoveryActions(err), "/settings")
		require.NotContains(t, recoveryActions(err), "/context")
		require.NotContains(t, recoveryActions(err), "/retry", "adjust the budget before requesting a new generation")
		p.recovery = recoveryState{err: err}
		require.Contains(t, p.composerPanel(), recoveryTitle(err))
		p.workspaceCommand("error", "")
		require.Contains(t, p.workspace.body, err.Error())
		p.closeWorkspace()
	}
}
