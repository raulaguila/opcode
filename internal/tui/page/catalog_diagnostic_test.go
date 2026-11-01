package page

import (
	"errors"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestCatalogDiagnosticDoesNotBecomeAnExecutionFailure(t *testing.T) {
	p, _ := workspacePage(t)
	executionError := errors.New("original execution failure")
	p.recovery = recoveryState{err: executionError}
	diagnostic := "TLS: check certificate and trust"
	p.Update(util.InfoMsg{Type: util.InfoTypeError, Msg: diagnostic, Details: diagnostic})
	require.Same(t, executionError, p.recovery.err, "catalog diagnostics cannot create or replace a retryable execution")
	p.workspaceCommand("error", "")
	require.Contains(t, p.workspace.body, diagnostic)
	require.Equal(t, config.Text("Detalhes da conexão", "Connection details"), p.workspace.title)
	p.closeWorkspace()
	newError := errors.New("new execution failure")
	p.Update(AgentOutcomeMsg{SessionID: "s", Error: newError})
	p.workspaceCommand("error", "")
	require.Contains(t, p.workspace.body, newError.Error())
	require.NotContains(t, p.workspace.body, diagnostic, "a later run failure owns its own diagnostic")
}
