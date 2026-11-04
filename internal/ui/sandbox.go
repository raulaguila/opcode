package ui

import (
	"cmp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
)

type sandboxMsg struct {
	err   error
	asked bool
}

// checkSandbox tests again, so a host fixed now needs no restart.
func (m *Model) checkSandbox() tea.Cmd {
	return func() tea.Msg { return sandboxMsg{err: shell.RecheckSandbox(), asked: true} }
}

func (m *Model) sandboxResult(msg sandboxMsg) {
	switch {
	case msg.err != nil && msg.asked:
		m.dialog = sandboxDialog{msg.err.Error()}
	case msg.err != nil:
		m.notice = cmp.Or(m.notice, textFor("Sandbox indisponível: comandos não rodam. /sandbox mostra causa e correção.", "Sandbox unavailable: commands will not run. /sandbox shows the cause and the fix."))
	case msg.asked:
		m.notice, m.info = "", textFor("Sandbox funcionando", "Sandbox working")
		if cfg := config.Get(); cfg != nil && cfg.Security.Sandbox == "none" {
			m.info = textFor("Sandbox desligado na configuração global", "Sandbox off in the global configuration")
		}
	}
}

// sandboxDialog shows the cause and the fix. opcode never applies the fix:
// both options change the machine's security, and that is the user's call.
type sandboxDialog struct{ text string }

func (d sandboxDialog) view(m *Model) string {
	box := Box{Title: textFor("Sandbox indisponível", "Sandbox unavailable"), Width: min(100, m.width-4), PadX: 2, PadY: 1,
		Legend: textFor("C copiar · Esc", "C copy · Esc")}
	inner := box.Inner()
	var rows []string
	for _, line := range strings.Split(ansi.Wrap(d.text, inner, ""), "\n") {
		rows = append(rows, Line(on(P.Surface, P.Text).Render(line), inner, P.Surface))
	}
	return box.Render(strings.Join(rows, "\n"))
}

func (d sandboxDialog) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "esc", "enter":
		m.dialog = nil
	case "c":
		return copyChatText(d.text), true
	}
	return nil, true
}
