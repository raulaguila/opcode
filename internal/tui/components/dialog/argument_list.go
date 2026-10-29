package dialog

import (
	"encoding/json"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"strings"
)

type argumentList struct {
	fields   []textinput.Model
	selected int
}

func newArgumentList(raw string) (*argumentList, error) {
	var values []string
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, err
		}
	}
	a := &argumentList{}
	for _, v := range values {
		a.add(v)
	}
	a.selected = 0
	a.focus()
	return a, nil
}
func (a *argumentList) add(v string) {
	f := textinput.New()
	f.Prompt = ""
	f.CharLimit = max(4096, len([]rune(v)))
	f.SetValue(v)
	a.fields = append(a.fields, f)
	a.selected = len(a.fields) - 1
	a.focus()
}
func (a *argumentList) focus() {
	for i := range a.fields {
		a.fields[i].Blur()
		if i == a.selected {
			a.fields[i].Focus()
		}
	}
}
func (a *argumentList) update(msg tea.Msg) (done, save bool, cmd tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return true, false, nil
		case "ctrl+s":
			return true, true, nil
		case "ctrl+n":
			if len(a.fields) < 64 {
				a.add("")
			}
			return false, false, textinput.Blink
		case "alt+backspace":
			if len(a.fields) > 0 {
				a.fields = append(a.fields[:a.selected], a.fields[a.selected+1:]...)
				a.selected = min(a.selected, max(0, len(a.fields)-1))
				a.focus()
			}
			return
		case "tab", "down", "shift+tab", "up":
			if len(a.fields) > 0 {
				delta := 1
				if k.String() == "up" || k.String() == "shift+tab" {
					delta = -1
				}
				a.selected = (a.selected + delta + len(a.fields)) % len(a.fields)
				a.focus()
			}
			return
		}
	}
	if len(a.fields) > 0 {
		a.fields[a.selected], cmd = a.fields[a.selected].Update(msg)
	}
	return
}
func (a *argumentList) value() string {
	values := make([]string, len(a.fields))
	for i, f := range a.fields {
		values[i] = f.Value()
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}
func (a *argumentList) view(width, height int) string {
	width = max(1, width)
	rows := max(1, height-9)
	start := max(0, a.selected-rows+1)
	lines := []string{config.Text("Argumentos · um por linha", "Arguments · one per row"), config.Text("Sem interpretação pelo shell; espaços pertencem ao argumento.", "No shell parsing; spaces belong to the argument.")}
	for i := start; i < min(len(a.fields), start+rows); i++ {
		a.fields[i].Width = max(1, width-4)
		mark := "  "
		if i == a.selected {
			mark = "› "
		}
		lines = append(lines, mark+a.fields[i].View())
	}
	if len(a.fields) == 0 {
		lines = append(lines, config.Text("Nenhum argumento. Ctrl+N adiciona.", "No arguments. Ctrl+N adds one."))
	}
	lines = append(lines, config.Text("Ctrl+N adiciona · Alt+Backspace remove", "Ctrl+N add · Alt+Backspace remove"), config.Text("Tab campo · Ctrl+S aplicar ao formulário · Esc cancelar", "Tab field · Ctrl+S apply to form · Esc cancel"))
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return styles.BaseStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).Render(strings.Join(lines, "\n"))
}
