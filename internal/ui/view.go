package ui

import (
	"context"
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
	"github.com/raulaguila/opcode/internal/llm/agent"
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
		if composer := m.composerView(); composer != "" {
			if len(m.mention) > 0 {
				parts = append(parts, m.mentionView())
			}
			parts = append(parts, composer)
		}
		parts = append(parts, m.footer())
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
	title := strings.TrimSpace(m.session.Title)
	if n := m.sessionNumber(); title != "" && n > 0 {
		title += config.Text(" · sessão ", " · session ") + fmt.Sprint(n)
	}
	right := muted.Render(title + " ")
	return Spread(left, right, m.width, P.Ground) + "\n" + on(P.Ground, P.Rule).Render(strings.Repeat("─", m.width))
}

// renderTranscript draws the conversation as the proposal does: your message
// on a heavy violet bar, tool steps as one muted line, replies on a thin blue
// bar with their model and duration.
func (m *Model) renderTranscript(width int) string {
	inner := max(10, width-gutter-3)
	if cfg := config.Get(); cfg != nil && cfg.TUI.ProseWidth > 0 {
		inner = min(inner, cfg.TUI.ProseWidth) // the reading width, as preferred
	}
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
			rows := stepsRows(steps, results, inner, m.approval != nil, P.Ground)
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
				meta := " " + modelName(msg.Model) + " · " + duration(msg.CreatedAt, finish.Time)
				if cites := agent.SourceCitations(text); len(cites) > 0 && cites[0].StartLine > 0 {
					meta += fmt.Sprintf(" · %s:%d", filepath.Base(cites[0].Path), cites[0].StartLine)
				}
				body += "\n" + on(P.Ground, P.Faint).Render(meta)
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
	if m.app != nil && !m.hasModel() {
		title = on(P.Ground, P.Strong).Bold(true).Render(config.Text("Configure uma conexão para começar", "Set up a connection to begin"))
		hint = on(P.Ground, P.Muted).Render(ansi.Truncate(config.Text("Ctrl+K › Configurações › Conexões · depois escolha o modelo com Ctrl+O", "Ctrl+K › Settings › Connections · then pick the model with Ctrl+O"), inner, "…"))
	}
	return "\n" + indent + title + "\n" + indent + hint + "\n"
}

// stepsRows folds tool calls into one line per run of the same tool and
// outcome: "› ✓ view store.go · main.go", "› ✗ edit store.go ×3 · 3 falhas".
// Older runs fold into a count so the turn stays short.
func stepsRows(calls []message.ToolCall, results map[string]message.ToolResult, width int, waiting bool, bg color.Color) []string {
	type run struct {
		name, state    string
		targets        []string
		added, removed int
	}
	unfold := config.Get() != nil && config.Get().Preferences.ProcessVisible() // one line per call
	var runs []run
	for _, call := range calls {
		state := "ok"
		added, removed := 0, 0
		if result, done := results[call.ID]; !done {
			state = "running"
		} else if result.IsError {
			state = "failed"
		} else {
			added, removed = editSize(result)
		}
		target := strings.TrimSpace(strings.TrimPrefix(toolBrief(call), call.Name))
		if n := len(runs); n > 0 && !unfold && runs[n-1].name == call.Name && runs[n-1].state == state {
			runs[n-1].targets = append(runs[n-1].targets, target)
			runs[n-1].added += added
			runs[n-1].removed += removed
			continue
		}
		runs = append(runs, run{call.Name, state, []string{target}, added, removed})
	}
	var rows []string
	const keep = 4
	if len(runs) > keep {
		earlier := 0
		for _, r := range runs[:len(runs)-keep+1] {
			earlier += len(r.targets)
		}
		rows = append(rows, on(bg, P.Muted).Render("› ")+on(bg, P.Success).Render("✓")+on(bg, P.Muted).Render(fmt.Sprintf(config.Text(" %d passos anteriores", " %d earlier steps"), earlier)))
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
				status = on(bg, P.Muted).Render(config.Text(" · aguardando sua aprovação", " · waiting for your approval"))
			}
		case "failed":
			icon, color = "✗", P.Danger
			unit := config.Text("falhas", "failures")
			if len(r.targets) == 1 {
				unit = config.Text("falha", "failure")
			}
			status = on(bg, P.Danger).Render(fmt.Sprintf(" · %d %s", len(r.targets), unit))
		case "ok":
			if size := sizeLabel(r.added, r.removed); size != "" {
				status = on(bg, P.Muted).Render(" · " + size)
			}
		}
		label = ansi.Truncate(label, max(1, width-4-ansi.StringWidth(ansi.Strip(status))), "…")
		rows = append(rows, on(bg, P.Muted).Render("› ")+on(bg, color).Render(icon)+on(bg, P.Muted).Render(" "+label)+status)
	}
	return rows
}

// editSize reads the lines an edit added and removed from its result.
func editSize(result message.ToolResult) (int, int) {
	var lines struct {
		Additions int `json:"additions"`
		Removals  int `json:"removals"`
	}
	if json.Unmarshal([]byte(result.Metadata), &lines) != nil {
		return 0, 0
	}
	return lines.Additions, lines.Removals
}

// sizeLabel is "+12 −3", "+12" or "".
func sizeLabel(added, removed int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", added))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("−%d", removed))
	}
	return strings.Join(parts, " ")
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
// composerRows is 0 while the agent asks: its card is where you answer.
func (m *Model) composerRows() int {
	if m.question != nil && !m.answeringLater() {
		return 0
	}
	return m.composer.Height() + 2
}

// composerView is the input framed in the mode's color, its name on the top
// edge and the keys on the bottom one.
func (m *Model) composerView() string {
	if m.composerRows() == 0 {
		return ""
	}
	accent, title := ModeColor(m.mode), modeLabel(m.mode)
	// While you decide on an approval, the composer only keeps your draft.
	m.composer.Placeholder = ""
	if m.approval != nil {
		accent, title = P.Border, ""
		m.composer.Placeholder = config.Text("Rascunho preservado enquanto você decide", "Draft kept while you decide")
	}
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
	if len(m.mention) > 0 {
		legend = config.Text("↑↓ escolher · Tab inserir · Esc fechar", "↑↓ choose · Tab insert · Esc close")
	}
	return Box{Title: title, Legend: legend, Width: m.width, PadX: 1, Accent: accent, Background: P.Ground, TitleColor: accent}.Render(strings.Join(lines, "\n"))
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
	case m.answeringLater():
		activity = on(P.Ground, P.Ask).Render("  " + config.Text("Pergunta pendente · Enter responde", "Question pending · Enter answers"))
	case m.question != nil:
		activity = on(P.Ground, P.Ask).Render("  " + config.Text("Aguardando sua resposta", "Waiting for your answer"))
	case m.approval != nil:
		activity = on(P.Ground, P.Plan).Render("  " + config.Text("Aguardando sua aprovação", "Waiting for your approval"))
	case m.busy:
		now := m.doing()
		activity = on(P.Ground, P.Plan).Render(fmt.Sprintf(config.Text("  %s · %s · Esc cancela", "  %s · %s · Esc cancels"), now, time.Since(m.started).Round(time.Second)))
		if compact {
			verb, _, _ := strings.Cut(now, " ")
			activity = on(P.Ground, P.Plan).Render(fmt.Sprintf("  %s %s", verb, time.Since(m.started).Round(time.Second)))
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
		right += Meter(used, P.Info, P.Ground) + muted.Render(fmt.Sprintf(" %d%%", int(used*100)))
		if speed := m.speed(); speed > 0 && !compact {
			right += muted.Render(fmt.Sprintf(" · %d t/s", int(speed+0.5)))
		}
		right += muted.Render("   ")
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

// doing names the step in progress, as the proposal's footer does:
// "Editando main.go", "Testando".
func (m *Model) doing() string {
	results := map[string]bool{}
	for _, msg := range m.messages {
		for _, result := range msg.ToolResults() {
			results[result.ToolCallID] = true
		}
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		calls := m.messages[i].ToolCalls()
		for j := len(calls) - 1; j >= 0; j-- {
			call := calls[j]
			if results[call.ID] {
				continue
			}
			target := strings.TrimSpace(strings.TrimPrefix(toolBrief(call), call.Name))
			switch {
			case strings.HasPrefix(call.Name, "edit"), strings.HasPrefix(call.Name, "write"), strings.HasPrefix(call.Name, "patch"), strings.HasPrefix(call.Name, "multiedit"):
				return strings.TrimSpace(config.Text("Editando ", "Editing ") + target)
			case call.Name == "bash" && strings.Contains(target, "test"):
				return config.Text("Testando", "Testing")
			case call.Name == "bash":
				return config.Text("Executando", "Running")
			case strings.HasPrefix(call.Name, "view"), strings.HasPrefix(call.Name, "read"):
				return strings.TrimSpace(config.Text("Lendo ", "Reading ") + target)
			case strings.HasPrefix(call.Name, "glob"), strings.HasPrefix(call.Name, "grep"), strings.HasPrefix(call.Name, "ls"), strings.HasPrefix(call.Name, "search"):
				return strings.TrimSpace(config.Text("Buscando ", "Searching ") + target)
			}
			return config.Text("Trabalhando", "Working")
		}
	}
	return config.Text("Trabalhando", "Working")
}

// speed is the last reply's tokens per second, when the agent measured it.
func (m *Model) speed() float64 {
	if m.app == nil || m.session.ID == "" {
		return 0
	}
	usage, ok := m.app.CoderAgent.(interface {
		Usage(context.Context, string) (agent.UsageMetrics, error)
	})
	if !ok {
		return 0
	}
	metrics, err := usage.Usage(context.Background(), m.session.ID)
	if err != nil || metrics.LastOutputTokens <= 0 || metrics.LastOutputDurationNS <= 0 {
		return 0
	}
	return float64(metrics.LastOutputTokens) / (float64(metrics.LastOutputDurationNS) / float64(time.Second))
}

// sessionNumber is the conversation's place, oldest first, among this
// workspace's conversations; it is counted once per conversation.
func (m *Model) sessionNumber() int {
	if m.session.ID == "" || m.app == nil || m.app.Sessions == nil {
		return 0
	}
	if m.numbered != m.session.ID {
		m.numbered, m.number = m.session.ID, 0
		if list, err := m.app.Sessions.List(context.Background()); err == nil {
			for _, item := range list {
				if item.ParentSessionID == "" && (item.CreatedAt < m.session.CreatedAt || item.ID == m.session.ID) {
					m.number++
				}
			}
		}
	}
	return m.number
}
