package core

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestFooterPhaseIsNotRepeatedBesideContextMetrics(t *testing.T) {
	m := statusCmp{width: 180}
	phase := config.Text("Aguardando modelo", "Awaiting model")
	state := util.FooterState{Busy: true, AwaitingModel: true, Activity: phase, ContextKnown: true, ContextWindow: 131072, ContextTokens: 100}
	view := ansi.Strip(m.ViewFor(state))
	require.Equal(t, 1, strings.Count(view, phase))
	require.NotContains(t, view, "tok/s", "waiting cannot present the previous request's speed")
	state.AwaitingModel, state.SpeedLive = false, true
	state.Activity = config.Text("Gerando resposta", "Generating response")
	state.TokensPerSecond = 70
	view = ansi.Strip(m.ViewFor(state))
	require.Equal(t, 1, strings.Count(view, state.Activity))
	require.Contains(t, view, "~70.0 tok/s")
}
