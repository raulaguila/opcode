package styles

import (
	"fmt"
	"image/color"
	"strings"
)

func getColorRGB(c color.Color) (uint8, uint8, uint8) {
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
func ForceReplaceBackgroundWithLipgloss(input string, newBgColor color.Color) string {
	count := 0
	for offset := 0; ; {
		_, end := nextBackgroundSGR(input, offset)
		if end < 0 {
			break
		}
		count++
		offset = end
	}
	if count == 0 {
		return input
	}
	r, g, b := getColorRGB(newBgColor)
	newBg := fmt.Sprintf("48;2;%d;%d;%d", r, g, b)
	var out strings.Builder
	// One buffer replaces per-SGR callback strings; the bound includes ESC[m's 0.
	out.Grow(len(input) + count*(len(newBg)+2))
	offset := 0
	for {
		start, end := nextBackgroundSGR(input, offset)
		if start < 0 {
			break
		}
		out.WriteString(input[offset:start])
		writeBackgroundSGR(&out, input, start, end, newBg)
		offset = end
	}
	out.WriteString(input[offset:])
	return out.String()
}

// Restore only changes that can disturb the surrounding surface background.
func restoreBackground(input string, bg color.Color) string {
	if input == "" {
		return ""
	}
	count := 0
	for offset := 0; ; {
		start, end := nextBackgroundSGR(input, offset)
		if end < 0 {
			break
		}
		if backgroundSGRAffects(input, start+2, end-1) {
			count++
		}
		offset = end
	}
	r, g, b := getColorRGB(bg)
	newBg := fmt.Sprintf("48;2;%d;%d;%d", r, g, b)
	var out strings.Builder
	out.Grow(len(input) + len(newBg) + 3 + count*(len(newBg)+2))
	out.WriteString("\x1b[")
	out.WriteString(newBg)
	out.WriteByte('m')
	offset := 0
	for {
		start, end := nextBackgroundSGR(input, offset)
		if start < 0 {
			break
		}
		out.WriteString(input[offset:start])
		if backgroundSGRAffects(input, start+2, end-1) {
			writeBackgroundSGR(&out, input, start, end, newBg)
		} else {
			out.WriteString(input[start:end])
		}
		offset = end
	}
	out.WriteString(input[offset:])
	return out.String()
}

func writeBackgroundSGR(out *strings.Builder, input string, start, end int, bg string) {
	out.WriteString("\x1b[")
	parameters := out.Len()
	rewriteBackgroundParameters(input, start+2, end-1, out)
	if out.Len() > parameters {
		out.WriteByte(';')
	}
	out.WriteString(bg)
	out.WriteByte('m')
}

func backgroundSGRAffects(raw string, start, end int) bool {
	if start == end || raw[end-1] == ';' {
		return true
	}
	for i := start; i < end; {
		parameterEnd := i
		advanceANSIParameter(raw, end, &parameterEnd)
		next, keep := backgroundSGRParameter(raw, i, parameterEnd, end)
		if !keep {
			return true
		}
		code, _, _ := strings.Cut(raw[i:parameterEnd], ":")
		isNum, value := true, 0
		parseANSIParameter(raw, i, i+len(code), &isNum, &value)
		if isNum && value == 0 {
			return true
		}
		i = next + 1
	}
	return false
}

// Recognize numeric SGR parameters, including colon color subparameters.
func nextBackgroundSGR(input string, offset int) (int, int) {
	for offset < len(input) {
		i := strings.Index(input[offset:], "\x1b[")
		if i < 0 {
			break
		}
		start := offset + i
		end := start + 2
		for end < len(input) && (input[end] == ';' || input[end] == ':' || input[end] >= '0' && input[end] <= '9') {
			end++
		}
		if end < len(input) && input[end] == 'm' {
			return start, end + 1
		}
		offset = start + 2
	}
	return -1, -1
}

func rewriteBackgroundParameters(raw string, start int, end int, sb *strings.Builder) {
	base := sb.Len()
	// An empty SGR parameter means reset, including the compact ESC[m form.
	if start == end {
		sb.WriteByte('0')
	}
	for i := start; i < end; {
		// find the next ';' or end
		j := i
		advanceANSIParameter(raw, end, &j)
		parameterEnd, keep := backgroundSGRParameter(raw, i, j, end)
		appendANSIParameter(sb, raw[i:parameterEnd], keep, base)
		// advance past this token (and the semicolon)
		i = parameterEnd + 1
	}
	if end > start && raw[end-1] == ';' {
		appendANSIParameter(sb, "0", true, base)
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

func appendANSIParameter(sb *strings.Builder, token string, keep bool, base int) {
	if keep {
		if token == "" {
			token = "0"
		}
		if sb.Len() > base {
			sb.WriteByte(';')
		}
		sb.WriteString(token)
	}
}

func backgroundSGRParameter(raw string, start, parameterEnd, end int) (int, bool) {
	code, _, colon := strings.Cut(raw[start:parameterEnd], ":")
	if !colon && (code == "38" || code == "48" || code == "58") {
		if next, ok := extendedSGRColorEnd(raw, parameterEnd, end); ok {
			return next, code != "48"
		}
	}
	if colon && code == "48" {
		return parameterEnd, false
	}
	isNum, value := true, 0
	parseANSIParameter(raw, start, start+len(code), &isNum, &value)
	keep := !isNum || ((value < 40 || value > 47) && (value < 100 || value > 107) && value != 49)
	return parameterEnd, keep
}

// A color's channel/index values are not independent background SGR parameters.
func extendedSGRColorEnd(raw string, separator, end int) (int, bool) {
	if separator+1 >= end {
		return separator, false
	}
	modeEnd := separator + 1
	advanceANSIParameter(raw, end, &modeEnd)
	channels := 0
	switch raw[separator+1 : modeEnd] {
	case "2":
		channels = 3
	case "5":
		channels = 1
	default:
		return separator, false
	}
	for range channels {
		if modeEnd >= end {
			break
		}
		modeEnd++
		advanceANSIParameter(raw, end, &modeEnd)
	}
	return modeEnd, true
}
