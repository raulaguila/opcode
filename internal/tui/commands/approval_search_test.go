package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApprovalSearchWorksWithPortugueseAndEnglishLabels(t *testing.T) {
	for _, entry := range Builtins {
		if entry.ID != "approval" {
			continue
		}
		for _, query := range []string{"aprovação", "aprovacao", "approval", "Change approval", "Alterar aprovação"} {
			require.True(t, Match(query, entry.ID, entry.Title+" "+entry.Description), query)
		}
		return
	}
	t.Fatal("approval command absent from the shared catalog")
}
