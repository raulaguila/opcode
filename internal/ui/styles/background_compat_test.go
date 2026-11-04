package styles

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"
)

func TestBackgroundRewriteMatchesLegacyOutsideCorrectedExtendedColors(t *testing.T) {
	fixtures := []string{"plain ação 漢字", "\x1b[m", "\x1b[1;;31m", "\x1b[1;m", "\x1b[;31m", "\x1b[2Jtext\x1b[?25l", "\x1b[\x1b[31m", "\x1b[1;38;2;12;23;34mUnicode ação 漢字\x1b[m", "\x1b[48;2;12;23;34m\x1b[48;5;255m", "\x1b[1;38;2;12;23", "\x1b[999999999999999999999999999999m"}
	for first := 0; first < 128; first++ {
		for _, second := range []int{0, 1, 4, 7, 22, 39, 41, 49, 100, 107} {
			fixtures = append(fixtures, fmt.Sprintf("prefix \x1b[%d;%dmcolored\x1b[m suffix", first, second))
		}
	}
	for _, bg := range []color.Color{color.RGBA{R: 22, G: 33, B: 44, A: 255}, color.NRGBA{R: 100, G: 107, B: 42, A: 128}} {
		for _, input := range fixtures {
			require.Equal(t, legacyForceReplaceBackgroundWithLipgloss(input, bg), ForceReplaceBackgroundWithLipgloss(input, bg), "input %q", input)
		}
	}
}

func TestBackgroundRewritePreservesExtendedForegroundAndUnderlineColors(t *testing.T) {
	const bg = "48;2;22;33;44"
	for _, params := range []string{"38;2;42;49;100", "38;5;41", "58;2;40;47;107", "58;5;49", "38:2::42:49:100", "38:5:41", "58:2:0:40:47:107", "58:5:49"} {
		input := "\x1b[1;" + params + ";4mvalue\x1b[m"
		want := "\x1b[1;" + params + ";4;" + bg + "mvalue\x1b[0;" + bg + "m"
		require.Equal(t, want, ForceReplaceBackgroundWithLipgloss(input, color.RGBA{R: 22, G: 33, B: 44, A: 255}))
	}
	for _, params := range []string{"48;2;42;49;100", "48;5;41", "48:2::42:49:100", "48:5:49"} {
		input := "\x1b[1;" + params + ";4mvalue"
		require.Equal(t, "\x1b[1;4;"+bg+"mvalue", ForceReplaceBackgroundWithLipgloss(input, color.RGBA{R: 22, G: 33, B: 44, A: 255}))
	}
}

func TestBackgroundRewriteKeepsActualForegroundAndSemanticAttributes(t *testing.T) {
	view := ForceReplaceBackgroundWithLipgloss("\x1b[1;3;4;38;2;42;49;100m漢\x1b[mp", color.RGBA{R: 22, G: 33, B: 44, A: 255})
	screen := vt10x.New(vt10x.WithSize(8, 2))
	_, err := screen.Write([]byte(view))
	require.NoError(t, err)
	cell := screen.Cell(0, 0)
	require.Equal(t, vt10x.Color(0x2a3164), cell.FG)
	require.Equal(t, vt10x.Color(0x16212c), cell.BG)
	require.EqualValues(t, 4|16|2, cell.Mode&(4|16|2))
	require.Zero(t, screen.Cell(2, 0).Mode&(4|16|2), "compact reset clears attributes after the CJK grapheme")
}

var backgroundRewriteBenchmarkResult string

func BenchmarkBackgroundRewrite(b *testing.B) {
	var bg color.Color = color.RGBA{R: 22, G: 33, B: 44, A: 255}
	for _, input := range []string{"plain Unicode ação 漢字", strings.Repeat("\x1b[1;38;5;246mvalue\x1b[m\n", 40)} {
		name := "plain"
		if strings.Contains(input, "\x1b[") {
			name = "styled"
		}
		b.Run(name, func(b *testing.B) {
			for _, test := range []struct {
				name    string
				rewrite func(string, color.Color) string
			}{{"legacy", legacyForceReplaceBackgroundWithLipgloss}, {"candidate", ForceReplaceBackgroundWithLipgloss}} {
				b.Run(test.name, func(b *testing.B) {
					b.ReportAllocs()
					for range b.N {
						backgroundRewriteBenchmarkResult = test.rewrite(input, bg)
					}
				})
			}
		})
	}
}

// Frozen c60aeb8 byte oracle; corrected extended-color behavior is tested above.
var legacyAnsiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func legacyGetColorRGB(c color.Color) (uint8, uint8, uint8) {
	r, g, b, a := c.RGBA()

	// Un-premultiply alpha if needed
	if a > 0 && a < 0xffff {
		r = (r * 0xffff) / a
		g = (g * 0xffff) / a
		b = (b * 0xffff) / a
	}

	// Convert from 16-bit to 8-bit color
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

// legacyForceReplaceBackgroundWithLipgloss replaces any ANSI background color codes
// in `input` with a single 24‑bit background (48;2;R;G;B).
func legacyForceReplaceBackgroundWithLipgloss(input string, newBgColor color.Color) string {
	// Precompute our new-bg sequence once
	r, g, b := legacyGetColorRGB(newBgColor)
	newBg := fmt.Sprintf("48;2;%d;%d;%d", r, g, b)

	return legacyAnsiEscape.ReplaceAllStringFunc(input, func(seq string) string {
		const (
			escPrefixLen = 2 // "\x1b["
			escSuffixLen = 1 // "m"
		)

		raw := seq
		start := escPrefixLen
		end := len(raw) - escSuffixLen

		var sb strings.Builder
		// reserve enough space: original content minus bg codes + our newBg
		sb.Grow((end - start) + len(newBg) + 2)

		// scan from start..end, token by token
		legacyRewriteBackgroundParameters(raw, start, end, &sb)

		// append our new background
		if sb.Len() > 0 {
			sb.WriteByte(';')
		}
		sb.WriteString(newBg)

		return "\x1b[" + sb.String() + "m"
	})
}

func legacyRewriteBackgroundParameters(raw string, start int, end int, sb *strings.Builder) {
	// An empty SGR parameter means reset, including the compact ESC[m form.
	if start == end {
		sb.WriteByte('0')
	}
	for i := start; i < end; {
		// find the next ';' or end
		j := i
		legacyAdvanceANSIParameter(raw, end, &j)
		token := raw[i:j]

		if next, ok := legacySkipExtendedBackground(raw, token, j, end); ok {
			i = next
			continue
		}

		// decide whether to keep this token
		// manually parse ASCII digits to int
		isNum := true
		val := 0
		legacyParseANSIParameter(raw, i, j, &isNum, &val)
		keep := !isNum ||
			((val < 40 || val > 47) && (val < 100 || val > 107) && val != 49)

		legacyAppendANSIParameter(sb, token, keep)
		// advance past this token (and the semicolon)
		i = j + 1
	}
	if end > start && raw[end-1] == ';' {
		legacyAppendANSIParameter(sb, "0", true)
	}
}

func legacySkipANSIRGBChannels(raw string, end int, m *int) {
	for count := 0; count < 3 && (*m) < end; count++ {
		for (*m) < end && raw[(*m)] != ';' {
			(*m)++
		}
		(*m)++
	}
}

func legacyParseANSIParameter(raw string, i int, j int, isNum *bool, val *int) {
	for p := i; p < j; p++ {
		c := raw[p]
		if c < '0' || c > '9' {
			(*isNum) = false
			break
		}
		(*val) = (*val)*10 + int(c-'0')
	}
}

func legacyAdvanceANSIParameter(raw string, end int, j *int) {
	for (*j) < end && raw[(*j)] != ';' {
		(*j)++
	}
}

func legacyAdvanceANSIToken(raw string, end int, l *int) {
	for (*l) < end && raw[(*l)] != ';' {
		(*l)++
	}
}

func legacyAdvanceANSIColorIndex(raw string, end int, m *int) {
	for (*m) < end && raw[(*m)] != ';' {
		(*m)++
	}
}

func legacyAppendANSIParameter(sb *strings.Builder, token string, keep bool) {
	if keep {
		if token == "" {
			token = "0"
		}
		if sb.Len() > 0 {
			sb.WriteByte(';')
		}
		sb.WriteString(token)
	}
}

func legacySkipExtendedBackground(raw, token string, j, end int) (int, bool) {
	if token != "48" || j+1 >= end {
		return 0, false
	}
	k, l := j+1, j+1
	legacyAdvanceANSIToken(raw, end, &l)
	switch raw[k:l] {
	case "5":
		m := l + 1
		legacyAdvanceANSIColorIndex(raw, end, &m)
		return m + 1, true
	case "2":
		m := l + 1
		legacySkipANSIRGBChannels(raw, end, &m)
		return m, true
	}
	return 0, false
}
