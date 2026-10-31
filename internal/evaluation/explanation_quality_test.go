package evaluation

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExplanationQualityFixturesPreserveEquivalentCriteriaAndCurrentSources(t *testing.T) {
	suite, raw, err := Load("testdata/explanation_quality.json")
	require.NoError(t, err)
	require.Len(t, suite.Cases, 4)
	require.Equal(t, suite.Cases[0].Files, suite.Cases[1].Files)
	require.Equal(t, suite.Cases[0].Criteria, suite.Cases[1].Criteria, "PT/EN wording must not change the quality gate")
	require.NotEqual(t, suite.Cases[0].Prompts, suite.Cases[1].Prompts)
	for _, c := range suite.Cases {
		require.Equal(t, "ask", c.Mode, "investigation fixtures never authorize edits or commands")
		for _, criterion := range c.Criteria {
			if criterion.Kind == "response_cites" {
				require.Contains(t, []string{"service.py", "tools_runtime.py"}, criterion.Path)
			}
		}
		base, root, err := Prepare(c)
		require.NoError(t, err)
		defer os.RemoveAll(base)
		wrong := Evaluate(c, root, Observation{Response: "Submit calls dispatchTool: TOOL_DENIED or TOOL_OK. (api_contract.py:3-4)"})
		require.Equal(t, "failed", wrong.Status, "facts without current implementation citations must fail")
		for _, criterion := range wrong.Criteria {
			if criterion.Kind == "response_cites" {
				require.False(t, *criterion.Passed)
			}
		}
		correct := Evaluate(c, root, Observation{Response: "Submit calls dispatchTool (service.py:4). TOOL_DENIED or TOOL_OK (tools_runtime.py:4-6)."})
		require.Equal(t, "passed", correct.Status)
	}
	report, err := Run(context.Background(), suite, raw, Options{})
	require.NoError(t, err)
	require.Equal(t, "offline_fixture_validation", report.Mode)
	for _, c := range report.Cases {
		require.Equal(t, "validated", c.Status)
		for _, criterion := range c.Criteria {
			require.Nil(t, criterion.Passed, "offline validation is not a model-quality result")
		}
	}
}
