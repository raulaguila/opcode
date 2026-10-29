package models

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReasoningChoicesRequireExplicitAdapterMetadata(t *testing.T) {
	m := Model{Provider: ProviderLocal, API: "responses", CanReason: true, ReasoningLevels: []string{"low", "high", "high", "\x1b[31m", "unlimited"}}
	require.Equal(t, []string{"low", "high"}, m.ReasoningChoices())
	m.API = "ollama"
	require.Empty(t, m.ReasoningChoices())
	m.API = "responses"
	m.CanReason = false
	require.Empty(t, m.ReasoningChoices())
	m.CanReason = true
	m.ReasoningLevels = nil
	require.Empty(t, m.ReasoningChoices())
}
