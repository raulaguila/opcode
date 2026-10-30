package styles

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func getColorRGB(c lipgloss.TerminalColor) (uint8, uint8, uint8) {
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

// ForceReplaceBackgroundWithLipgloss replaces any ANSI background color codes
// in `input` with a single 24‑bit background (48;2;R;G;B).
func ForceReplaceBackgroundWithLipgloss(input string, newBgColor lipgloss.TerminalColor) string {
	// Precompute our new-bg sequence once
	r, g, b := getColorRGB(newBgColor)
	newBg := fmt.Sprintf("48;2;%d;%d;%d", r, g, b)

	return ansiEscape.ReplaceAllStringFunc(input, func(seq string) string {
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
		rewriteBackgroundParameters(raw, start, end, &sb)

		// append our new background
		if sb.Len() > 0 {
			sb.WriteByte(';')
		}
		sb.WriteString(newBg)

		return "\x1b[" + sb.String() + "m"
	})
}

func rewriteBackgroundParameters(raw string, start int, end int, sb *strings.Builder) {
	for i := start; i < end; {
		// find the next ';' or end
		j := i
		advanceANSIParameter(raw, end, &j)
		token := raw[i:j]

		if next, ok := skipExtendedBackground(raw, token, j, end); ok {
			i = next
			continue
		}

		// decide whether to keep this token
		// manually parse ASCII digits to int
		isNum := true
		val := 0
		parseANSIParameter(raw, i, j, &isNum, &val)
		keep := !isNum ||
			((val < 40 || val > 47) && (val < 100 || val > 107) && val != 49)

		appendANSIParameter(sb, token, keep)
		// advance past this token (and the semicolon)
		i = j + 1
	}
}

func skipANSIRGBChannels(raw string, end int, m *int) {
	for count := 0; count < 3 && (*m) < end; count++ {
		for (*m) < end && raw[(*m)] != ';' {
			(*m)++
		}
		(*m)++
	}
}

func parseANSIParameter(raw string, i int, j int, isNum *bool, val *int) {
	for p := i; p < j; p++ {
		c := raw[p]
		if c < '0' || c > '9' {
			(*isNum) = false
			break
		}
		(*val) = (*val)*10 + int(c-'0')
	}
}

func advanceANSIParameter(raw string, end int, j *int) {
	for (*j) < end && raw[(*j)] != ';' {
		(*j)++
	}
}

func advanceANSIToken(raw string, end int, l *int) {
	for (*l) < end && raw[(*l)] != ';' {
		(*l)++
	}
}

func advanceANSIColorIndex(raw string, end int, m *int) {
	for (*m) < end && raw[(*m)] != ';' {
		(*m)++
	}
}

func appendANSIParameter(sb *strings.Builder, token string, keep bool) {
	if keep {
		if sb.Len() > 0 {
			sb.WriteByte(';')
		}
		sb.WriteString(token)
	}
}

func skipExtendedBackground(raw, token string, j, end int) (int, bool) {
	if token != "48" || j+1 >= end {
		return 0, false
	}
	k, l := j+1, j+1
	advanceANSIToken(raw, end, &l)
	switch raw[k:l] {
	case "5":
		m := l + 1
		advanceANSIColorIndex(raw, end, &m)
		return m + 1, true
	case "2":
		m := l + 1
		skipANSIRGBChannels(raw, end, &m)
		return m, true
	}
	return 0, false
}
