package tools

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNaturalQueryRanksExactIdentifiersBeforeBodyMentions(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "a_decoy.go", "package fixture\nfunc Irrelevant() string { return \"como executeToolBatch results process tools\" }\n")
	retrievalFixture(t, root, "batch.go", "package fixture\nfunc executeToolBatch() string { return \"ACTUAL_EXECUTION\" }\n")
	retrievalFixture(t, root, "batch_test.go", "package fixture\nfunc TestExecution() { executeToolBatch() }\n")
	tool := SecureTool(NewCodeIndexTool(), nil)
	for _, query := range []string{"como executeToolBatch recebe results?", "Explain executeToolBatch cancellation"} {
		page := searchPage(t, tool, map[string]any{"query": query, "intent": "implementation", "limit": 1})
		require.Len(t, page.Results, 1)
		require.Equal(t, "batch.go", page.Results[0].Path)
		require.Equal(t, "executeToolBatch", page.Results[0].Symbol)
	}
}

func TestQualifiedIdentifierSuffixRankingRetainsScopeAndKinds(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "api/receiver.go", "package fixture\ntype Agent struct{}\nfunc (a *Agent) SubmitBatch() string {return \"API_IMPLEMENTATION\"}\n")
	retrievalFixture(t, root, "other/receiver.go", "package fixture\nfunc SubmitBatch() {}\n")
	page := searchPage(t, SecureTool(NewCodeIndexTool(), nil), map[string]any{"query": "Explain Agent.SubmitBatch results", "path": "api", "intent": "implementation", "limit": 1})
	require.Len(t, page.Results, 1)
	require.Equal(t, "api/receiver.go", page.Results[0].Path)
}

func TestCodeIdentifierArgumentsAreBoundedAndParameterized(t *testing.T) {
	values := codeQueryIdentifiers("Explain execute_tool and Agent.SubmitBatch; ignore sql') OR 1=1")
	require.Contains(t, values, "execute_tool")
	require.Contains(t, values, "agent.submitbatch")
	order, args := codeIdentifierOrder("Explain executeToolBatch and Agent.SubmitBatch")
	require.NotContains(t, order, "executeToolBatch")
	require.Contains(t, args, "executetoolbatch")
	require.Contains(t, args, ".agent.submitbatch")
}

func TestImplementationIntentPrefersExecutableDeclarationsAfterExactIdentifiers(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "a_contract.go", "package fixture\ntype Service interface { SetMode(string) error }\n")
	retrievalFixture(t, root, "mode.go", "package fixture\ntype Agent struct{}\nfunc (a *Agent) SetMode(mode string) error { return nil }\n")
	tool := SecureTool(NewCodeIndexTool(), nil)
	for _, intent := range []string{"implementation", "overview"} {
		page := searchPage(t, tool, map[string]any{"query": "SetMode", "intent": intent, "limit": 1})
		require.Len(t, page.Results, 1)
		require.Equal(t, "mode.go", page.Results[0].Path)
		require.Equal(t, "Agent.SetMode", page.Results[0].Symbol)
		page = searchPage(t, tool, map[string]any{"query": "Service", "intent": intent, "limit": 1})
		require.Len(t, page.Results, 1)
		require.Equal(t, "Service", page.Results[0].Symbol, "explicit declarations still rank before unrelated bodies")
	}
}

func TestTestIntentPrefersTestPathsBeforeImplementationIdentifierAndRetainsScope(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "mode.go", "package fixture\nfunc SetMode() {}\n")
	retrievalFixture(t, root, "api/mode_test.go", "package fixture\nfunc TestModeApproval() { SetMode() }\n")
	retrievalFixture(t, root, "other/mode_test.go", "package fixture\nfunc TestSetMode() { SetMode() }\n")
	tool := SecureTool(NewCodeIndexTool(), nil)
	page := searchPage(t, tool, map[string]any{"query": "SetMode", "intent": "tests", "path": "api", "limit": 1})
	require.Len(t, page.Results, 1)
	require.Equal(t, "api/mode_test.go", page.Results[0].Path)
	page = searchPage(t, tool, map[string]any{"query": "SetMode", "intent": "tests", "limit": 1})
	require.Len(t, page.Results, 1)
	require.True(t, IsTestSource(page.Results[0].Path), "exact implementation must not steal test discovery")
	page = searchPage(t, tool, map[string]any{"query": "NothingExists", "intent": "tests", "path": "api", "limit": 1})
	require.Empty(t, page.Results, "missing evidence does not synthesize tests")
	page = searchPage(t, tool, map[string]any{"query": "SetMode", "intent": "tests", "path": "mode.go", "limit": 1})
	require.Len(t, page.Results, 1)
	require.Equal(t, "mode.go", page.Results[0].Path, "test preference cannot widen an explicit file scope")
}
