package agent

import (
	"testing"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestFlowGuidancePreservesExecutableEvidenceAndEveryRequestedBranchDuringRepair(t *testing.T) {
	run := &generationRun{turnMessageID: "original"}
	history := []message.Message{
		{ID: "original", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Qual handler é chamado neste projeto? Leia o caminho de execução e explique todos os retornos."}}},
		{ID: "repair", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Application validation: fix references", Origin: "application"}}},
	}
	guidance := flowResponseGuidance(run, history)
	for _, phrase := range []string{"executable call/return statement lines", "not only a declaration header", "every input/result explicitly requested", "all requested branches when repairing", "complete original answer", "separate input → result pairs", "citation beside each pair"} {
		require.Contains(t, guidance, phrase)
	}
	require.Empty(t, flowResponseGuidance(&generationRun{turnMessageID: "repair"}, history))
}

func TestOverviewResponseGuidanceSurvivesCitationRepairWithoutApplyingToSpecificQuestions(t *testing.T) {
	run := &generationRun{turnMessageID: "user"}
	history := []message.Message{{ID: "user", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "o que pode me falar sobre esse projeto?"}}}, {ID: "repair", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Application validation: fix the source reference", Origin: "application"}}}}
	guidance := overviewResponseGuidance(run, history)
	require.Contains(t, guidance, "practical capabilities")
	require.Contains(t, guidance, "when repairing citations")
	require.Contains(t, guidance, "one explicit sentence")
	require.Empty(t, overviewResponseGuidance(&generationRun{turnMessageID: "repair"}, history))
}
