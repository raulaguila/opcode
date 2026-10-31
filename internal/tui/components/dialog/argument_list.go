package dialog

import (
	"encoding/json"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
)

type argumentList struct {
	pairs    bool
	note     string
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
	f := newDialogInput()
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
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return true, false, nil
		case "ctrl+s":
			return updateArgumentListCtrlS(a)
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
			updateFields(a, &k)
			return
		}
	}
	if len(a.fields) > 0 {
		a.fields[a.selected], cmd = a.fields[a.selected].Update(msg)
	}
	return
}
func (a *argumentList) value() string {
	if a.pairs {
		values := map[string]string{}
		for _, f := range a.fields {
			k, v, _ := strings.Cut(f.Value(), "=")
			values[k] = v
		}
		raw, _ := json.Marshal(values)
		return string(raw)
	}
	values := make([]string, len(a.fields))
	for i, f := range a.fields {
		values[i] = f.Value()
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}
func (a *argumentList) view(width, height int) string {
	width = max(1, width)
	rows := max(1, height-10)
	start := max(0, a.selected-rows+1)
	lines := []string{config.Text("Argumentos · um por linha", "Arguments · one per row"), config.Text("Sem interpretação pelo shell; espaços pertencem ao argumento.", "No shell parsing; spaces belong to the argument.")}
	if a.pairs {
		lines = []string{config.Text("Variáveis · nome=VARIAVEL", "Variables · name=VARIABLE"), config.Text("Informe nomes de variáveis de ambiente, sem segredos", "Enter environment variable names, without secrets")}
	}
	for i := start; i < min(len(a.fields), start+rows); i++ {
		a.fields[i].SetWidth(max(1, width-4))
		mark := "  "
		if i == a.selected {
			mark = "› "
		}
		lines = append(lines, mark+a.fields[i].View())
	}
	if len(a.fields) == 0 {
		lines = append(lines, config.Text("Nenhum argumento. Ctrl+N adiciona.", "No arguments. Ctrl+N adds one."))
	}
	lines = append(lines, styles.PanelDivider(width), config.Text("Ctrl+N adiciona · Alt+Backspace remove", "Ctrl+N add · Alt+Backspace remove"), config.Text("Tab campo · Ctrl+S aplicar ao formulário · Esc cancelar", "Tab field · Ctrl+S apply to form · Esc cancel"))
	if a.note != "" {
		lines = append(lines, a.note)
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return styles.Panel().Padding(1, 2).Border(lipgloss.NormalBorder()).Render(strings.Join(lines, "\n"))
}

func newPairList(raw string) (*argumentList, error) {
	values := map[string]string{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, err
		}
	}
	a := &argumentList{pairs: true}
	keys := []string{}
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a.add(k + "=" + values[k])
	}
	a.selected = 0
	a.focus()
	return a, nil
}

// ItemEditor reuses the literal list UI for task targets and acceptance items.
type ItemEditor struct{ list *argumentList }

func NewItemEditor(items []string) *ItemEditor {
	a := &argumentList{}
	for _, v := range items {
		a.add(v)
	}
	a.selected = 0
	a.focus()
	return &ItemEditor{a}
}
func (e *ItemEditor) Update(msg tea.Msg) (bool, bool, tea.Cmd) { return e.list.update(msg) }
func (e *ItemEditor) Items() []string {
	items := []string{}
	for _, f := range e.list.fields {
		if v := strings.TrimSpace(f.Value()); v != "" {
			items = append(items, v)
		}
	}
	return items
}
func (e *ItemEditor) View(w, h int) string { return e.list.view(w, h) }

func updateArgumentListCtrlS(a *argumentList) (extractedResult0 bool, extractedResult1 bool, extractedResult2 tea.Cmd) {
	if a.pairs {
		seen := map[string]bool{}
		for _, f := range a.fields {
			k, v, ok := strings.Cut(f.Value(), "=")
			if !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" || seen[k] {
				a.note = config.Text("Use nome=VARIAVEL, sem nomes repetidos", "Use name=VARIABLE, without duplicate names")
				return false, false, nil
			}
			seen[k] = true
		}
	}
	return true, true, nil

}

func updateFields(a *argumentList, k *tea.KeyPressMsg) {
	if len(a.fields) > 0 {
		delta := 1
		if k.String() == "up" || k.String() == "shift+tab" {
			delta = -1
		}
		a.selected = (a.selected + delta + len(a.fields)) % len(a.fields)
		a.focus()
	}
}
