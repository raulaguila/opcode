package core

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestFooterDropsTokenFractionBeforeClippingRate(t *testing.T) {
	state := util.FooterState{ContextWindow: 16384, ContextTokens: 4096, ContextKnown: true, TokensPerSecond: 110.2, Activity: "Concluído"}
	m := statusCmp{width: 110}
	short := m.metricsFor(state, true)
	require.Contains(t, short, "110.2 tok/s")
	_, cw, mw, _ := m.metricWidths(state)
	require.GreaterOrEqual(t, mw, ansi.StringWidth(m.metrics(state))+1, "metrics fit their segment")
	require.Greater(t, cw, 0)
	require.NotContains(t, ansi.Strip(m.ViewFor(state)), "tok…")
}
