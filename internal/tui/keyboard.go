package tui

import (
	"bytes"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/raulaguila/opcode/internal/tui/util"
)

// Keyboard bridges enhanced terminal keys to Bubble Tea v1's legacy key events.
// It enables disambiguation only after a terminal acknowledges CSI-u support.
type Keyboard struct {
	mu                     sync.Mutex
	output                 io.Writer
	probe, queried, pushed bool
}

var _ term.File = (*Keyboard)(nil)

func NewKeyboard(output io.Writer) *Keyboard {
	k := &Keyboard{output: output}
	if file, ok := output.(interface{ Fd() uintptr }); ok {
		k.probe = term.IsTerminal(file.Fd())
	}
	return k
}

func (k *Keyboard) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.closeLocked()
}

func (k *Keyboard) closeLocked() error {
	k.queried = false
	if !k.pushed {
		return nil
	}
	k.pushed = false
	_, err := io.WriteString(k.output, ansi.PopKittyKeyboard(1))
	return err
}

func (k *Keyboard) Fd() uintptr {
	if file, ok := k.output.(interface{ Fd() uintptr }); ok {
		return file.Fd()
	}
	return ^uintptr(0)
}

func (k *Keyboard) Read(raw []byte) (int, error) {
	if reader, ok := k.output.(io.Reader); ok {
		return reader.Read(raw)
	}
	return 0, io.EOF
}

func (k *Keyboard) Write(raw []byte) (int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	// Kitty keeps separate stacks for the main and alternate screens. Pop while
	// still in the alternate screen, including external-editor and error exits.
	if bytes.Contains(raw, []byte("\x1b[?1049l")) {
		_ = k.closeLocked()
	}
	return k.output.Write(raw)
}

func (k *Keyboard) Filter(_ tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		k.mu.Lock()
		if k.probe && !k.queried {
			k.queried = true
			_, _ = io.WriteString(k.output, ansi.RequestKittyKeyboard)
		}
		k.mu.Unlock()
	}
	typ := reflect.TypeOf(msg)
	if typ == nil || typ.PkgPath() != "github.com/charmbracelet/bubbletea" {
		return msg
	}
	// v1 keeps unrecognized CSI events private. Confine that compatibility
	// boundary here; ordinary keys and bracketed paste are never reinterpreted.
	if typ.Name() != "unknownCSISequenceMsg" || typ.Kind() != reflect.Slice || typ.Elem().Kind() != reflect.Uint8 {
		return msg
	}
	raw := string(reflect.ValueOf(msg).Bytes())
	if strings.HasPrefix(raw, "\x1b[?") && strings.HasSuffix(raw, "u") {
		if flags, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(raw, "\x1b[?"), "u")); err == nil && flags >= 0 && flags <= 31 {
			k.mu.Lock()
			if k.probe && k.queried && !k.pushed {
				if _, err := io.WriteString(k.output, ansi.PushKittyKeyboard(1)); err == nil {
					k.pushed = true
				}
			}
			supported := k.pushed
			k.mu.Unlock()
			return util.EnhancedKeyboardMsg{Supported: supported}
		}
	}
	if key, ok := enhancedKey(raw); ok {
		return key
	}
	return msg
}

func enhancedKey(raw string) (tea.Msg, bool) {
	if !strings.HasPrefix(raw, "\x1b[") {
		return nil, false
	}
	body := strings.TrimPrefix(raw, "\x1b[")
	var code, modifier string
	switch {
	case strings.HasSuffix(body, "u"):
		parts := strings.Split(strings.TrimSuffix(body, "u"), ";")
		if len(parts) < 1 || len(parts) > 2 {
			return nil, false
		}
		code, modifier = parts[0], "1"
		if len(parts) == 2 {
			modifier = parts[1]
		}
		if strings.Contains(modifier, ":") {
			fields := strings.Split(modifier, ":")
			if len(fields) != 2 {
				return nil, false
			}
			if fields[1] == "3" {
				return nil, true
			} // Releases must not submit twice.
			if fields[1] != "1" && fields[1] != "2" {
				return nil, false
			}
			modifier = fields[0]
		}
	case strings.HasSuffix(body, "~"):
		parts := strings.Split(strings.TrimSuffix(body, "~"), ";")
		if len(parts) != 3 || parts[0] != "27" {
			return nil, false
		}
		modifier, code = parts[1], parts[2]
	default:
		return nil, false
	}
	n, err := strconv.Atoi(code)
	if err != nil || n < 0 || n > unicode.MaxRune || n >= 0xd800 && n <= 0xdfff {
		return nil, false
	}
	mod, err := strconv.Atoi(modifier)
	if err != nil || mod < 1 || mod > 256 {
		return nil, false
	}
	bits := (mod - 1) &^ 192 // Caps/Num Lock do not change shortcut modifiers.
	if bits&^7 != 0 {
		return nil, false
	}
	key := tea.KeyMsg{Alt: bits&2 != 0}
	switch n {
	case 57350, 57351: // CSI-u left/right functional keys.
		types := []tea.KeyType{tea.KeyLeft, tea.KeyShiftLeft, tea.KeyCtrlLeft, tea.KeyCtrlShiftLeft}
		if n == 57351 {
			types = []tea.KeyType{tea.KeyRight, tea.KeyShiftRight, tea.KeyCtrlRight, tea.KeyCtrlShiftRight}
		}
		index := 0
		if bits&1 != 0 {
			index++
		}
		if bits&4 != 0 {
			index += 2
		}
		key.Type = types[index]
	case 13:
		key.Type = tea.KeyEnter
		if bits&5 != 0 {
			// Legacy Ctrl+Enter is Ctrl+J; keep that mapping for other panels.
			key.Type = tea.KeyCtrlJ
		}
	case 9:
		key.Type = tea.KeyTab
		if bits&1 != 0 {
			key.Type = tea.KeyShiftTab
		}
	case 27:
		key.Type = tea.KeyEsc
	case 127:
		key.Type = tea.KeyBackspace
	default:
		if bits&4 != 0 {
			if n < 32 || n > 127 {
				return nil, false
			}
			key.Type = tea.KeyType(n & 31)
		} else if n < 32 {
			key.Type = tea.KeyType(n)
		} else {
			if n >= 57344 && n <= 63743 {
				return nil, false
			} // Kitty functional-key range.
			r := rune(n)
			if bits&1 != 0 {
				r = unicode.ToUpper(r)
			}
			key.Type, key.Runes = tea.KeyRunes, []rune{r}
		}
	}
	return key, true
}
