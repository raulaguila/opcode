package evaluation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsePatternsValidateBeforeUseAndRejectMalformedDirectCalls(t *testing.T) {
	for _, kind := range []string{"response_matches", "response_not_matches"} {
		for _, pattern := range []string{"", "(", strings.Repeat("a", 2001)} {
			require.Error(t, validateCriterion("pattern", Criterion{Kind: kind, Value: pattern}))
		}
		require.NoError(t, validateCriterion("pattern", Criterion{Kind: kind, Value: `(?i)\bOK\b`}))
		require.False(t, responseMatches("answer", Criterion{Kind: kind, Value: "("}))
		require.False(t, responseMatches("answer", Criterion{Kind: kind}))
	}
	require.True(t, responseMatches("ok", Criterion{Kind: "response_matches", Value: `(?i)\bOK\b`}))
	require.False(t, responseMatches("notokay", Criterion{Kind: "response_matches", Value: `(?i)\bOK\b`}))
	require.True(t, responseMatches("different", Criterion{Kind: "response_not_matches", Value: `(?i)\bOK\b`}))
	require.False(t, responseMatches("OK", Criterion{Kind: "response_not_matches", Value: `(?i)\bOK\b`}))
}
