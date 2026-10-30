package page

import (
	"errors"
	"fmt"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
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
