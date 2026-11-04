package styles

import (
	"fmt"
	"image/color"
	"strings"
)

// ForceReplaceBackgroundWithLipgloss exercises the live SGR parser and writer.
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
