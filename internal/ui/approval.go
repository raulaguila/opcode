package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
)

// diffRows is how many changed lines the approval card previews.
const diffRows = 6

// approvalView is the proposal's approval card: it sits right above the
// composer, names the request on its top edge, previews the change and
// offers numbered choices.
func (m *Model) approvalView() string {
	request := m.approval
	title := config.Text("Aprovação necessária", "Approval required")
	scope := config.Text("2 Permitir este escopo na sessão", "2 Allow this scope for the session")
	var diff, command, file string
	switch params := request.Params.(type) {
	case tools.EditPermissionsParams:
		diff, file = params.Diff, params.FilePath
	case tools.WritePermissionsParams:
		diff, file = params.Diff, params.FilePath
	case tools.BashPermissionsParams:
		command = params.Command
	}
	switch request.ToolName {
	case tools.EditToolName, tools.WriteToolName, tools.PatchToolName:
		title = config.Text("Aprovar edição", "Approve edit")
		scope = config.Text("2 Permitir edições no projeto nesta sessão", "2 Allow project edits this session")
	case tools.BashToolName:
		title = config.Text("Aprovar comando", "Approve command")
	case tools.FetchToolName:
		title = config.Text("Aprovar acesso à rede", "Approve network access")
	}
	bg := P.ApprovalCard
	box := Box{Title: title, Meta: request.ToolName, Width: m.width - 2*gutter + 2, PadX: 2, PadY: 1, Accent: P.Plan, TitleColor: P.Plan, Background: bg, Outer: P.Ground}
	inner := box.Inner()
	preview, added, removed := diffPreview(diff, inner)
	if diff != "" {
		box.Meta = fmt.Sprintf(config.Text("1 arquivo · +%d −%d", "1 file · +%d −%d"), added, removed)
	}
	head := on(bg, P.Strong).Bold(true).Render(request.Description)
	if file != "" {
		// The description repeats the absolute path; the card names the file.
		name := file
		if rel, err := filepath.Rel(config.WorkingDirectory(), file); err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
		head = on(bg, P.Strong).Bold(true).Render(name) + on(bg, P.Muted).Render("  "+strings.TrimSpace(strings.ReplaceAll(request.Description, file, "")))
	}
	lines := []string{head}
	if diff == "" && request.Path != "" {
		lines = append(lines, on(bg, P.Muted).Render(config.Text("Escopo: ", "Scope: ")+homeRelative(request.Path)))
	}
	if command != "" {
		lines = append(lines, "", Line(on(P.Field, P.Faint).Render(" $ ")+on(P.Field, P.Strong).Render(strings.Join(strings.Fields(command), " ")), inner, P.Field))
	}
	if len(preview) > 0 {
		lines = append(append(lines, ""), preview...)
	}
	gap := on(bg, bg).Render("  ")
	lines = append(lines, "",
		Button(config.Text("1 Permitir uma vez", "1 Allow once"), true, P.Plan)+gap+
			Button(scope, false, P.Plan)+gap+
			on(P.Raised, P.Danger).Render(" "+config.Text("3 Recusar · Esc", "3 Deny · Esc")+" "),
		on(bg, P.Muted).Render(config.Text("Comandos, rede, exclusões e caminhos fora do projeto continuam pedindo aprovação.", "Commands, network, deletions and outside paths still ask for approval.")),
	)
	for i := range lines {
		lines[i] = Line(lines[i], inner, bg)
	}
	return indentBlock(box.Render(strings.Join(lines, "\n")))
}

// diffPreview draws the first changed lines of a unified diff with their
// line numbers, on the field ground, and counts additions and removals.
func diffPreview(diff string, width int) (rows []string, added, removed int) {
	newLine, oldLine, shown, hidden := 0, 0, 0, 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "@@"):
			// "@@ -12,3 +12,8 @@": read where both sides start.
			for _, field := range strings.Fields(line) {
				start, _, _ := strings.Cut(field[1:], ",")
				if n, err := strconv.Atoi(start); err == nil && field[0] == '+' {
					newLine = n
				} else if err == nil && field[0] == '-' {
					oldLine = n
				}
			}
			continue
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		case line == "" || line[0] == '\\' || newLine == 0 && oldLine == 0:
			continue
		}
		bg, fg, numberColor, number := P.Field, P.Faint, P.Faint, newLine
		switch line[0] {
		case '+':
			bg, fg, numberColor = P.Added, P.AddedText, P.AddedNumber
			newLine++
		case '-':
			bg, fg, numberColor, number = P.Removed, P.RemovedText, P.RemovedText, oldLine
			oldLine++
		default:
			newLine++
			oldLine++
		}
		if shown >= diffRows {
			hidden++
			continue
		}
		shown++
		text := strings.ReplaceAll(line, "\t", "    ")
		rows = append(rows, Line(on(bg, numberColor).Render(fmt.Sprintf("%5d  ", number))+on(bg, fg).Render(ansi.Strip(text)), width, bg))
	}
	if hidden > 0 {
		rows = append(rows, Line(on(P.Field, P.Faint).Render(fmt.Sprintf(config.Text("    …  mais %d linhas", "    …  %d more lines"), hidden)), width, P.Field))
	}
	if len(rows) > 0 {
		rows = append([]string{Line("", width, P.Field)}, append(rows, Line("", width, P.Field))...)
	}
	return rows, added, removed
}
