package theme

import (
	"encoding/json"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveColorPreservesLegacyJSONAndUsesExplicitAppearance(t *testing.T) {
	previous := HasDarkBackground()
	t.Cleanup(func() { SetHasDarkBackground(previous) })
	const legacy = `{"Light":"#123456","Dark":"#abcdef"}`
	var value AdaptiveColor
	require.NoError(t, json.Unmarshal([]byte(legacy), &value))
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, legacy, string(encoded))
	for _, dark := range []bool{true, false} {
		SetHasDarkBackground(dark)
		want := value.Light
		if dark {
			want = value.Dark
		}
		r, g, b, a := value.RGBA()
		wr, wg, wb, wa := lipgloss.Color(want).RGBA()
		require.Equal(t, []uint32{wr, wg, wb, wa}, []uint32{r, g, b, a})
	}
}
