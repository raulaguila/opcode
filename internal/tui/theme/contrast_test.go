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

func TestOpcodeSemanticColorsHaveContrastInBothThemes(t *testing.T) {
	palette := NewOpcodeTheme()
	colors := map[string]AdaptiveColor{"text": palette.Text(), "muted": palette.TextMuted(), "selected": palette.Primary(), "secondary": palette.Secondary(), "accent": palette.Accent(), "warning": palette.Warning(), "error": palette.Error(), "success": palette.Success(), "info": palette.Info()}
	luminance := func(hex string) float64 {
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
	for name, fg := range colors {
		for _, bg := range []AdaptiveColor{palette.Background(), palette.BackgroundSecondary()} {
			for mode, pair := range map[string][2]string{"light": {fg.Light, bg.Light}, "dark": {fg.Dark, bg.Dark}} {
				a, b := luminance(pair[0]), luminance(pair[1])
				require.GreaterOrEqual(t, (max(a, b)+0.05)/(min(a, b)+0.05), 4.5, name+" "+mode+" "+pair[1])
			}
		}
	}
}
