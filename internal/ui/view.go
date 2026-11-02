package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/tui/styles"
)

const (
	headerRows = 2 // title bar and its rule
	footerRows = 1
	gutter     = 2 // left margin of the transcript
)

func (m *Model) View() tea.View {
	body := ""
	if m.width > 0 && m.height > 0 {
		parts := []string{m.header(), m.transcript.View()}
		if work := m.workView(); work != "" {
			parts = append(parts, work)
		}
		parts = append(parts, m.composerView(), m.footer())
		body = lipgloss.NewStyle().Background(P.Ground).Width(m.width).Height(m.height).MaxHeight(m.height).Render(strings.Join(parts, "\n"))
		body = m.overlay(body)
	}
	view := tea.NewView(styles.TerminalOutput(body))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.ReportFocus = true
	return view
}

func modeLabel(mode string) string {
	switch mode {
	case "ask":
		return config.Text("Perguntar", "Ask")
	case "plan":
		return config.Text("Planejar", "Plan")
	}
	return config.Text("Executar", "Execute")
}

// header: product, project and branch on the left, the conversation title
// on the right, then a rule.
func (m *Model) header() string {
	muted := on(P.Ground, P.Muted)
	left := on(P.Ground, P.Strong).Bold(true).Render(" opcode")
	if config.Get() != nil {
		left += muted.Render(" · " + homeRelative(config.WorkingDirectory()))
		if branch := gitBranch(config.WorkingDirectory()); branch != "" {
			left += muted.Render(" · ") + on(P.Ground, P.Success).Render(branch)
		}
	}
	right := muted.Render(strings.TrimSpace(m.session.Title) + " ")
	return Spread(left, right, m.width, P.Ground) + "\n" + on(P.Ground, P.Rule).Render(strings.Repeat("─", m.width))
}

// renderTranscript draws the conversation as the proposal does: your message
// on a heavy violet bar, tool steps as one muted line, replies on a thin blue
// bar with their model and duration.
func (m *Model) renderTranscript(width int) string {
	inner := max(10, width-gutter-3)
	indent := on(P.Ground, P.Ground).Render(strings.Repeat(" ", gutter))
	results := map[string]message.ToolResult{}
	for _, msg := range m.messages {
		for _, result := range msg.ToolResults() {
			results[result.ToolCallID] = result
		}
	}
	// A turn shows one reply: when the model revises its answer (citation
	// repairs), only the last public text is drawn.
	latest := map[int]string{}
	turn := 0
	for _, msg := range m.messages {
		if msg.Role == message.User && !msg.IsApplication() {
			turn++
		}
		if msg.Role == message.Assistant && strings.TrimSpace(msg.Content().String()) != "" {
			latest[turn] = msg.ID
		}
	}
	turn = 0
	var blocks []string
	var steps []message.ToolCall
	failureAt := -1 // the recovery card follows the last steps of the conversation
	flush := func() {
		if len(steps) > 0 {
			rows := stepsRows(steps, results, inner, m.approval != nil)
			for i := range rows {
				rows[i] = indent + rows[i]
			}
			blocks = append(blocks, strings.Join(rows, "\n"))
			failureAt = len(blocks)
			steps = nil
		}
	}
	for _, msg := range m.messages {
		if msg.IsApplication() || msg.Role == message.Tool {
			continue
		}
		text := strings.TrimSpace(msg.Content().String())
		if msg.Role == message.User {
			turn++
			flush()
			blocks = append(blocks, barred(text, "┃", P.User, P.Strong, inner, indent))
			continue
		}
		if text != "" && latest[turn] == msg.ID {
			flush()
			body := strings.TrimSuffix(styles.RenderMarkdownBlocks(text, inner, inner), "\n")
			if finish := msg.FinishPart(); finish != nil && msg.IsFinished() {
				body += "\n" + on(P.Ground, P.Faint).Render(" "+modelName(msg.Model)+" · "+duration(msg.CreatedAt, finish.Time))
			}
			blocks = append(blocks, barredRendered(body, "│", P.Ask, indent))
		}
		steps = append(steps, msg.ToolCalls()...)
	}
	flush()
	if m.failure != nil {
		if failureAt < 0 {
			failureAt = len(blocks)
		}
		blocks = slices.Insert(blocks, failureAt, m.failureView())
	}
	if len(blocks) == 0 {
		return m.emptyState(inner, indent)
	}
	return "\n" + strings.Join(blocks, "\n\n") + "\n"
}

func (m *Model) emptyState(inner int, indent string) string {
	title := on(P.Ground, P.Strong).Bold(true).Render(config.Text("O que vamos fazer neste projeto?", "What shall we do in this project?"))
	hint := on(P.Ground, P.Muted).Render(ansi.Truncate(config.Text("Shift+Tab alterna Perguntar, Planejar e Executar · / comandos", "Shift+Tab cycles Ask, Plan and Execute · / commands"), inner, "…"))
	return "\n" + indent + title + "\n" + indent + hint + "\n"
}

// stepsRows folds tool calls into one line per run of the same tool and
// outcome: "› ✓ view store.go · main.go", "› ✗ edit store.go ×3 · 3 falhas".
// Older runs fold into a count so the turn stays short.
func stepsRows(calls []message.ToolCall, results map[string]message.ToolResult, width int, waiting bool) []string {
	type run struct {
		name, state string
		targets     []string
	}
	var runs []run
	for _, call := range calls {
		state := "ok"
		if result, done := results[call.ID]; !done {
			state = "running"
		} else if result.IsError {
			state = "failed"
		}
		target := strings.TrimSpace(strings.TrimPrefix(toolBrief(call), call.Name))
		if n := len(runs); n > 0 && runs[n-1].name == call.Name && runs[n-1].state == state {
			runs[n-1].targets = append(runs[n-1].targets, target)
			continue
		}
		runs = append(runs, run{call.Name, state, []string{target}})
	}
	var rows []string
	const keep = 4
	if len(runs) > keep {
		earlier := 0
		for _, r := range runs[:len(runs)-keep+1] {
			earlier += len(r.targets)
		}
		rows = append(rows, on(P.Ground, P.Muted).Render("› ")+on(P.Ground, P.Success).Render("✓")+on(P.Ground, P.Muted).Render(fmt.Sprintf(config.Text(" %d passos anteriores", " %d earlier steps"), earlier)))
		runs = runs[len(runs)-keep+1:]
	}
	for _, r := range runs {
		// The same target repeated reads "×3"; different ones are listed once each.
		var names []string
		for _, target := range r.targets {
			if target != "" && !slices.Contains(names, target) {
				names = append(names, target)
			}
		}
		label := r.name
		if len(names) > 0 {
			label += " " + strings.Join(names, " · ")
		}
		if len(names) == 1 && len(r.targets) > 1 {
			label += fmt.Sprintf(" ×%d", len(r.targets))
		}
		icon, color, status := "✓", P.Success, ""
		switch r.state {
		case "running":
			icon, color = "●", P.Plan
			if waiting {
				status = on(P.Ground, P.Muted).Render(config.Text(" · aguardando sua aprovação", " · waiting for your approval"))
			}
		case "failed":
			icon, color = "✗", P.Danger
			unit := config.Text("falhas", "failures")
			if len(r.targets) == 1 {
				unit = config.Text("falha", "failure")
			}
			status = on(P.Ground, P.Danger).Render(fmt.Sprintf(" · %d %s", len(r.targets), unit))
		}
		label = ansi.Truncate(label, max(1, width-4-ansi.StringWidth(ansi.Strip(status))), "…")
		rows = append(rows, on(P.Ground, P.Muted).Render("› ")+on(P.Ground, color).Render(icon)+on(P.Ground, P.Muted).Render(" "+label)+status)
	}
	return rows
}

// toolBrief names a call by its tool and main argument, e.g. "edit store.go".
func toolBrief(call message.ToolCall) string {
	var input map[string]any
	_ = json.Unmarshal([]byte(call.Input), &input)
	for _, field := range []string{"file_path", "path", "command", "pattern", "query", "url"} {
		if value, ok := input[field].(string); ok && strings.TrimSpace(value) != "" {
			if field == "file_path" || field == "path" {
				value = filepath.Base(value)
			}
			return call.Name + " " + ansi.Truncate(strings.Join(strings.Fields(value), " "), 40, "…")
		}
	}
	return call.Name
}

// barred wraps plain text beside a colored bar.
func barred(text, bar string, barColor, textColor color.Color, width int, indent string) string {
	var rows []string
	for _, line := range strings.Split(ansi.Wrap(text, width, ""), "\n") {
		rows = append(rows, indent+on(P.Ground, barColor).Render(bar)+on(P.Ground, textColor).Render(" "+line))
	}
	return strings.Join(rows, "\n")
}

func barredRendered(body, bar string, barColor color.Color, indent string) string {
	var rows []string
	for _, line := range strings.Split(body, "\n") {
		rows = append(rows, indent+on(P.Ground, barColor).Render(bar)+line)
	}
	return strings.Join(rows, "\n")
}

func modelName(id models.ModelID) string {
	if model, ok := models.SupportedModels[id]; ok {
		return modelLabel(model)
	}
	return string(id)
}

func duration(from, to int64) string {
	if to <= from {
		return "<1s"
	}
	return (time.Duration(to-from) * time.Second).String()
}

// composerRows: the frame's two edges around the text.
func (m *Model) composerRows() int { return m.composer.Height() + 2 }

// composerView is the input framed in the mode's color, its name on the top
// edge and the keys on the bottom one.
func (m *Model) composerView() string {
	accent := ModeColor(m.mode)
	lines := strings.Split(m.composer.View(), "\n")
	for i := range lines {
		mark := "  "
		if i == 0 {
			mark = on(P.Ground, accent).Bold(true).Render("❯") + on(P.Ground, P.Text).Render(" ")
		}
		lines[i] = mark + lines[i]
	}
	legend := config.Text("Shift+Tab modo · / comandos · @ arquivos · Alt+Enter nova linha", "Shift+Tab mode · / commands · @ files · Alt+Enter new line")
	if m.busy {
		legend = config.Text("Mensagens entram na fila · Esc cancela", "Messages queue up · Esc cancels")
	}
	return Box{Title: modeLabel(m.mode), Legend: legend, Width: m.width, PadX: 1, Accent: accent, Background: P.Ground, TitleColor: accent}.Render(strings.Join(lines, "\n"))
}

// footer: approval on the left, the run's activity, then context and model.
func (m *Model) footer() string {
	muted := on(P.Ground, P.Muted)
	label := config.Text("Aprov.: pedir ▾", "Approval: ask ▾")
	if m.policy == permission.ApprovalProjectEdits {
		label = config.Text("Aprov.: editar no projeto ▾", "Approval: project edits ▾")
	}
	compact := m.width < 80 // the proposal's 64-column screen abbreviates
	if compact {
		label = config.Text("A:pedir", "A:ask")
		if m.policy == permission.ApprovalProjectEdits {
			label = config.Text("A:editar", "A:edits")
		}
	}
	approval := on(P.Ground, P.Text).Render(" " + label)
	activity := ""
	switch {
	case m.question != nil:
		activity = on(P.Ground, P.Ask).Render("  " + config.Text("Aguardando sua resposta", "Waiting for your answer"))
	case m.approval != nil:
		activity = on(P.Ground, P.Plan).Render("  " + config.Text("Aguardando sua aprovação", "Waiting for your approval"))
	case m.busy:
		activity = on(P.Ground, P.Plan).Render(fmt.Sprintf(config.Text("  Trabalhando · %s · Esc cancela", "  Working · %s · Esc cancels"), time.Since(m.started).Round(time.Second)))
		if compact {
			activity = on(P.Ground, P.Plan).Render(fmt.Sprintf(config.Text("  Trabalhando %s", "  Working %s"), time.Since(m.started).Round(time.Second)))
		}
	case m.notice != "":
		activity = on(P.Ground, P.Danger).Render("  " + strings.Join(strings.Fields(m.notice), " "))
	case m.info != "":
		activity = on(P.Ground, P.Success).Render("  " + m.info)
	}
	right := ""
	model := models.Model{}
	if m.app != nil && m.app.CoderAgent != nil {
		model = m.app.CoderAgent.Model()
	}
	if window := model.ContextWindow; window > 0 && m.session.PromptTokens > 0 {
		used := float64(m.session.PromptTokens) / float64(window)
		right += Meter(used, P.Info, P.Ground) + muted.Render(fmt.Sprintf(" %d%%   ", int(used*100)))
	}
	name := modelName(model.ID)
	if compact {
		name = ansi.Truncate(name, 9, "…")
	}
	right += on(P.Ground, P.Text).Render(name+" ▾") + " "
	return Spread(approval+activity, right, m.width, P.Ground)
}

func homeRelative(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// gitBranch reads .git/HEAD (following a worktree's gitdir file).
func gitBranch(dir string) string {
	git := filepath.Join(dir, ".git")
	if raw, err := os.ReadFile(git); err == nil {
		target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
		if !ok {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		git = target
	}
	raw, err := os.ReadFile(filepath.Join(git, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if ref, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return ref
	}
	return ansi.Truncate(head, 7, "")
}
