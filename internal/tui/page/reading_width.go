package page

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func appendReadingActions(p *chatPage) {
	cfg := config.Get()
	if cfg == nil {
		return
	}
	for _, preset := range []struct {
		width  int
		pt, en string
	}{
		{0, "Largura total", "Full width"},
		{96, "Leitura confortável · 96 colunas", "Comfortable reading · 96 columns"},
	} {
		label := config.Text(preset.pt, preset.en)
		if cfg.TUI.ProseWidth == preset.width {
			label += " ✓"
		}
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: label, kind: "reading-width", text: strconv.Itoa(preset.width)})
	}
}

func (p *chatPage) setReadingWidth(width string) tea.Cmd {
	if err := config.SaveUserSetting("tui.proseWidth", width, false); err != nil {
		return util.ReportError(err)
	}
	for i, item := range p.workspace.actions {
		if item.kind == "reading-width" {
			p.workspace.actions[i].label = strings.TrimSuffix(item.label, " ✓")
			if item.text == width {
				p.workspace.actions[i].label += " ✓"
			}
		}
	}
	p.workspace.note = config.Text("Largura de leitura salva; código e tabelas mantêm a largura disponível.", "Reading width saved; code and tables retain available width.")
	return util.CmdHandler(chat.ReadingWidthChangedMsg{})
}
