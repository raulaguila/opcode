package commands

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSharedSearchUsesPortugueseEnglishAndCanonicalIDs(t *testing.T) {
	for _, query := range []string{"permissoes", "Permissões", "/permissions", "approvals"} {
		require.True(t, Match(query, "permissions", ""), query)
	}
	require.True(t, Match("contexto", "context", ""))
	require.False(t, Match("desconhecido", "context", ""))
	require.Less(t, Rank("/plan", "plan"), Rank("/plan", "resume"))
}
