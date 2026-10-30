package chat

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const normalizedSearchLimit = 2 << 20

type searchLine struct {
	raw, plain string
	bytes      int
}

func (m *messagesCmp) normalizedSearchLine(row int, line string) string {
	f := &m.find
	if row < len(f.normalized) && f.normalized[row].raw == line {
		return f.normalized[row].plain
	}
	plain := strings.ToLower(ansi.Strip(line))
	size := len(line) + len(plain) + 32
	if row >= 10000 || size > normalizedSearchLimit {
		return plain
	}
	if row < len(f.normalized) {
		f.normalizedBytes -= f.normalized[row].bytes
		f.normalized[row] = searchLine{}
	}
	if f.normalizedBytes+size > normalizedSearchLimit {
		return plain
	}
	for len(f.normalized) <= row {
		f.normalized = append(f.normalized, searchLine{})
	}
	f.normalized[row] = searchLine{strings.Clone(line), strings.Clone(plain), size}
	f.normalizedBytes += size
	return plain
}
