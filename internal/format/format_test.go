package format

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONCarriesTheOutcomeForAutomation(t *testing.T) {
	require.Equal(t, "ok", FormatOutput(Result{Response: "ok", Outcome: "partial"}, "text"))
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(FormatOutput(Result{Response: "ok", Outcome: "partial", SessionID: "s"}, "json")), &got))
	require.Equal(t, "partial", got["outcome"])
	require.Equal(t, "s", got["session_id"])
	require.Equal(t, []any{}, got["files_changed"], "an empty list, not null")
	require.Equal(t, []any{}, got["checks"])
}
