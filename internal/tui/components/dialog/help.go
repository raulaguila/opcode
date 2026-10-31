package dialog

import (
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/util"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
)

type helpCmp struct {
	width    int
	height   int
	keys     []key.Binding
	viewport viewport.Model
}

func (h *helpCmp) Init() tea.Cmd {
	return nil
}

func (h *helpCmp) SetBindings(k []key.Binding) {
	h.keys = k
}

func (h *helpCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.Code == tea.KeyEnd {
			h.viewport.GotoBottom()
			return h, nil
		}
		if msg.Code == tea.KeyHome {
			h.viewport.GotoTop()
			return h, nil
		}
	case tea.WindowSizeMsg:
		h.width = min(90, max(8, msg.Width-4))
		h.height = msg.Height
		h.viewport.SetWidth(max(1, h.width-4))
		h.viewport.SetHeight(max(1, msg.Height-9))
	}
	var cmd tea.Cmd
	h.viewport, cmd = h.viewport.Update(msg)
	return h, cmd
}

func removeDuplicateBindings(bindings []key.Binding) []key.Binding {
	seen := make(map[string]struct{})
	result := make([]key.Binding, 0, len(bindings))

	// Process bindings in reverse order
	for _, b := range slices.Backward(bindings) {

		k := strings.Join(b.Keys(), " ")
		if _, ok := seen[k]; ok {
			// duplicate, skip
			continue
		}
		seen[k] = struct{}{}
		// Add to the beginning of result to maintain original order
		result = append([]key.Binding{b}, result...)
	}

	return result
}

func (h *helpCmp) render() string {
	width := max(1, h.width-4)
	lines := []string{}
	groups := [][]string{{}, {}, {}}
	for _, binding := range removeDuplicateBindings(h.keys) {
		d := strings.ToLower(binding.Help().Desc)
		group := 0
		if strings.Contains(d, "terminal") || strings.Contains(d, "process") || strings.Contains(d, "plan") || strings.Contains(d, "task") {
			group = 1
		}
		if strings.Contains(d, "model") || strings.Contains(d, "theme") || strings.Contains(d, "settings") || strings.Contains(d, "help") {
			group = 2
		}
		groups[group] = append(groups[group], ansi.Wrap(binding.Help().Key+"  "+interfaceCopy(binding.Help().Desc), width, ""))
	}
	for i, group := range groups {
		if len(group) == 0 {
			continue
		}
		lines = append(lines, []string{config.Text("Conversa e edição", "Conversation and editing"), config.Text("Trabalho e terminal", "Work and terminal"), config.Text("Configurações e ajuda", "Settings and help")}[i])
		lines = append(lines, group...)
		lines = append(lines, "")
	}

	h.viewport.SetWidth(width)
	if h.viewport.Height() == 0 {
		h.viewport.SetHeight(10)
	}
	h.viewport.SetContent(strings.Join(lines, "\n"))
	return h.viewport.View()
}
func (h *helpCmp) View() string {
	title := config.Text("Atalhos do teclado", "Keyboard shortcuts")
	hint := config.Text("↑↓/PgUp/PgDn rola · Esc fecha", "↑↓/PgUp/PgDn scroll · Esc closes")
	return styles.Panel().Width(max(1, h.width)).Padding(1).Render(strings.Join([]string{styles.PanelTitle().Render(title), h.render(), styles.PanelDivider(max(1, h.width-4)), ansi.Truncate(hint, max(1, h.width-4), "…")}, "\n"))
}

type HelpCmp interface {
	util.Model
	SetBindings([]key.Binding)
}

func NewHelpCmp() HelpCmp {
	return &helpCmp{}
}
