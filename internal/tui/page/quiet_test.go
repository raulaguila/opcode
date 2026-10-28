package page

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestQuietChatKeepsPlanOnDemandAndQuestionsVisible(t *testing.T) {
	p := workflowPage(t)
	_, err := p.app.Workflows.UpdatePlan(context.Background(), "s", "Task", []workflow.Step{{Title: "Check", Status: "in_progress"}}, []string{"Pass checks"}, nil)
	require.NoError(t, err)
	p.refreshWorkflow()
	require.Zero(t, p.workflowHeight(), "a new plan must not take over the composer")
	require.Empty(t, p.workflowView())
	p.workflow.expanded = true
	require.Positive(t, p.workflowHeight())
	require.Contains(t, p.workflowView(), "Check")
	p.workflow.expanded = false
	p.workflow.question.ID = "question"
	p.workflow.question.Question = "Choose a database"
	require.Positive(t, p.workflowHeight(), "questions must remain visible")
	require.Contains(t, p.workflowView(), "Choose a database")
}
