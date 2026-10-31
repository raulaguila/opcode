package evaluation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseCitationRequiresTheDeclaredImplementationLine(t *testing.T) {
	files := map[string]string{"src/service.py": "def dispatchTool(text):\n    if text == 'blocked':\n        return 'TOOL_DENIED'\n    return 'TOOL_OK'\n"}
	c := Criterion{Kind: "response_cites", Path: "src/service.py", Value: "3"}
	for _, answer := range []string{"src/service.py:3", "Denied (`src/service.py:2-3`).", "src/service.py:1 src/service.py:3", "[src/service.py:2-4]", "./src/service.py:3"} {
		require.True(t, responseCitesAnchor(files, answer, c), answer)
	}
	for _, answer := range []string{"src/service.py", "src/service.py:1", "src/service.py:1-999", "src/service.py:0-3", "src/service.py:4-3", "src/service.py:3-bad", "src/service.py:3-4-5", "src/service.py:30", "other/src/service.py:3", "other.src/service.py:3", "SRC/service.py:3", "src/service.py:3garbage", "src/service.py:99999999999999999999999999"} {
		require.False(t, responseCitesAnchor(files, answer, c), answer)
	}
	// A valid reference alone is coverage, not a claim of native read or semantic proof.
	result := Evaluate(Case{ID: "coverage", Files: files, Criteria: []Criterion{c}}, "unused", Observation{Response: "src/service.py:3"})
	require.Equal(t, "passed", result.Status)
}

func TestCitationCriteriaRejectInvalidOrMissingFixtureAnchors(t *testing.T) {
	base := Case{ID: "coverage", Mode: "ask", Files: map[string]string{"source.py": "first\nsecond\n"}, Prompts: []string{"Explain"}}
	for _, c := range []Criterion{
		{Kind: "response_cites", Path: "source.py", Value: "0"},
		{Kind: "response_cites", Path: "source.py", Value: "03"},
		{Kind: "response_cites", Path: "source.py", Value: "-1"},
		{Kind: "response_cites", Path: "source.py", Value: "1-2"},
		{Kind: "response_cites", Path: "source.py", Value: "3"},
		{Kind: "response_cites", Path: "missing.py", Value: "1"},
		{Kind: "response_cites", Path: "../source.py", Value: "1"},
	} {
		base.Criteria = []Criterion{c}
		require.Error(t, base.validate(), c)
	}
	base.Criteria = []Criterion{{Kind: "response_cites", Path: "source.py", Value: "2"}}
	require.NoError(t, base.validate())
}
