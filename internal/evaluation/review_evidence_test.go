package evaluation

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewEvidenceRequiresImplementationAndRegressionReferences(t *testing.T) {
	suite, _, err := Load("testdata/review_evidence.json")
	require.NoError(t, err)
	require.Len(t, suite.Cases, 2)
	require.Equal(t, suite.Cases[0].Files, suite.Cases[1].Files)
	require.Equal(t, suite.Cases[0].Criteria, suite.Cases[1].Criteria)
	for _, c := range suite.Cases {
		require.Equal(t, "ask", c.Mode)
		base, root, err := Prepare(c)
		require.NoError(t, err)
		defer os.RemoveAll(base)
		tools := map[string]bool{"search_code_index": true}
		good := Evaluate(c, root, Observation{Tools: tools, Response: "SetMode already rejects an active request with ErrSessionBusy (runtime.go:6). The tests check rejection and the idle transition to execute (runtime_test.go:5-7). Tests were inspected, not executed."})
		require.Equal(t, "passed", good.Status)
		declarations := Evaluate(c, root, Observation{Tools: tools, Response: "SetMode should reject busy requests with ErrSessionBusy (agent.go:2)."})
		require.Equal(t, "failed", declarations.Status)
		missingTests := Evaluate(c, root, Observation{Tools: tools, Response: "SetMode rejects ErrSessionBusy (runtime.go:6)."})
		require.Equal(t, "failed", missingTests.Status)
	}
}
