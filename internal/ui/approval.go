package ui

import (
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
)

// approvalView is the proposal's approval card: it sits right above the
// composer, names the request on its top edge and offers numbered choices.
func (m *Model) approvalView() string {
	request := m.approval
	title := config.Text("Aprovação necessária", "Approval required")
	switch request.ToolName {
	case tools.EditToolName, tools.WriteToolName, tools.PatchToolName:
		title = config.Text("Aprovar edição", "Approve edit")
	case tools.BashToolName:
		title = config.Text("Aprovar comando", "Approve command")
	case tools.FetchToolName:
		title = config.Text("Aprovar acesso à rede", "Approve network access")
	}
	box := Box{Title: title, Meta: request.ToolName, Width: m.width - 2*gutter + 2, PadX: 2, PadY: 1, Accent: P.Plan, TitleColor: P.Plan}
	inner := box.Inner()
	lines := []string{
		on(P.Surface, P.Strong).Bold(true).Render(request.Description),
		on(P.Surface, P.Muted).Render(config.Text("Escopo: ", "Scope: ") + request.Path),
		"",
		Button(config.Text("1 Permitir uma vez", "1 Allow once"), true, P.Plan) + on(P.Surface, P.Text).Render("  ") +
			Button(config.Text("2 Permitir este escopo na sessão", "2 Allow this scope for the session"), false, P.Plan) + on(P.Surface, P.Text).Render("  ") +
			on(P.Surface, P.Danger).Render(" "+config.Text("3 Recusar · Esc", "3 Deny · Esc")+" "),
		on(P.Surface, P.Muted).Render(config.Text("Comandos, rede, exclusões e caminhos fora do projeto continuam pedindo aprovação.", "Commands, network, deletions and outside paths still ask for approval.")),
	}
	for i := range lines {
		lines[i] = Line(lines[i], inner, P.Surface)
	}
	indent := strings.Repeat(" ", gutter-1)
	var out []string
	for _, row := range strings.Split(box.Render(strings.Join(lines, "\n")), "\n") {
		out = append(out, on(P.Ground, P.Ground).Render(indent)+row)
	}
	return strings.Join(out, "\n")
}

func (m *Model) approvalRows() int {
	if m.approval == nil {
		return 0
	}
	return 9
}
