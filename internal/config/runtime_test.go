package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRuntimeDefaultsAndInvalidSettings(t *testing.T) {
	defaults := (RuntimeConfig{}).Defaults()
	require.Equal(t, "execute", defaults.Mode)
	require.Equal(t, 32, defaults.MaxSteps)
	require.Equal(t, 300, defaults.FirstTokenTimeout)
	require.NoError(t, defaults.Validate())
	for _, value := range []RuntimeConfig{{Mode: "unsafe"}, {MaxSteps: -1}, {MaxSteps: 201}, {MaxSeconds: 7201}, {ToolBudget: 100}, {FirstTokenTimeout: -1}, {IdleTimeout: 601}} {
		require.Error(t, value.Validate())
	}
	require.NoError(t, (RuntimeConfig{Mode: "plan", MaxSteps: 20, MaxSeconds: 600, ToolBudget: 24000, FirstTokenTimeout: 120, IdleTimeout: 30}).Validate())
}
