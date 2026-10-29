package theme

import (
	"github.com/stretchr/testify/require"
	"math"
	"strconv"
	"testing"
)

func TestOpcodeMutedTextRemainsReadableOnBothBackgrounds(t *testing.T) {
	palette := NewOpcodeTheme()
	for _, colors := range [][2]string{
		{palette.TextMutedColor.Dark, palette.BackgroundColor.Dark},
		{palette.TextMutedColor.Light, palette.BackgroundColor.Light},
	} {
		luma := func(hex string) float64 {
			n, err := strconv.ParseUint(hex[1:], 16, 24)
			require.NoError(t, err)
			var result float64
			for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
				v := float64((n>>uint(16-i*8))&255) / 255
				if v <= 0.04045 {
					v /= 12.92
				} else {
					v = math.Pow((v+0.055)/1.055, 2.4)
				}
				result += weight * v
			}
			return result
		}
		a, b := luma(colors[0]), luma(colors[1])
		require.GreaterOrEqual(t, (max(a, b)+0.05)/(min(a, b)+0.05), 4.5)
	}
}
