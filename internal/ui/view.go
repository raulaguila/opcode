package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/fileutil"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/transcript"
	"github.com/raulaguila/opcode/internal/ui/styles"
)

const (
	headerRows = 2 // title bar and its rule
	footerRows = 2 // a row of air under the composer's keys, then the footer
	gutter     = 2 // left margin of the transcript
)

func (m *Model) View() tea.View {
	body := ""
	if m.width > 0 && m.height > 0 {
		var below []string
		if work := m.workView(); work != "" {
			below = append(below, work)
		}
		if composer := m.composerView(); composer != "" {
			if len(m.mention) > 0 {
				below = append(below, m.mentionView())
			}
			below = append(below, composer)
		}
		below = append(below, m.footer())
		body = m.screen(m.header(), m.transcriptRows(), below)
		body = m.overlay(body)
	}
	view := tea.NewView(styles.TerminalOutput(body))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.ReportFocus = true
	return view
}

// transcriptRows cuts the visible rows out of the painted transcript; the
// viewport keeps only the scroll position, since restyling every visible
// row each frame was most of the cost of drawing the screen.
func (m *Model) transcriptRows() []string {
	from := min(max(0, m.transcript.YOffset()), len(m.rows))
	return m.highlightSelection(m.rows[from:min(len(m.rows), from+m.transcript.Height())], from)
}

// screen stacks the header, the transcript's visible rows (already painted
// to the width) and the parts below into exactly the window's rows; a short
// row of the other parts is filled with the ground.
func (m *Model) screen(header string, transcript, below []string) string {
	blank := Line("", m.width, P.Ground)
	fit := func(rows []string) []string {
		for i, row := range rows {
			if ansi.StringWidth(row) != m.width {
				rows[i] = Line(row, m.width, P.Ground)
			}
		}
		return rows
	}
	rows := fit(strings.Split(header, "\n"))
	rows = append(rows, transcript...)
	for range m.transcript.Height() - len(transcript) {
		rows = append(rows, blank)
	}
	rows = append(rows, fit(strings.Split(strings.Join(below, "\n"), "\n"))...)
	rows = rows[:min(len(rows), m.height)]
	for len(rows) < m.height {
		rows = append(rows, blank)
	}
	return strings.Join(rows, "\n")
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
		left += muted.Render(" · " + fileutil.HomeRelative(config.WorkingDirectory()))
		if branch := fileutil.GitBranch(config.WorkingDirectory()); branch != "" {
			left += muted.Render(" · ") + on(P.Ground, P.Success).Render(branch)
		}
	}
	title := strings.TrimSpace(m.session.Title)
	if n := m.sessionNumber(); title != "" && n > 0 {
		title += config.Text(" · sessão ", " · session ") + fmt.Sprint(n)
	}
	if title != "" {
		title += " ▾"
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
	p := transcriptPaint{m: m, inner: inner, indent: on(P.Ground, P.Ground).Render(strings.Repeat(" ", gutter)), fresh: map[string]renderedBlock{}, failureAt: -1}
	p.results, p.started = toolTimeline(m.messages)
	p.commands = m.commandEntries()
	p.commandByID = map[string]commandEntry{}
	for _, entry := range p.commands {
		p.commandByID[entry.id] = entry
	}
	// Messages render once; only the reply still streaming renders again.
	defer func() { m.blocks = p.fresh }()
	p.conversation()
	p.managedCommands()
	// While the model thinks, its reasoning shows live; the answer replaces it.
	if thought := message.LiveThought(m.messages, 4000); m.busy && thought != "" {
		rows := strings.Split(ansi.Wrap(thought, inner-1, ""), "\n")
		rows = append([]string{textFor("Pensando…", "Thinking…")}, rows[max(0, len(rows)-6):]...)
		p.blocks = append(p.blocks, barred(strings.Join(rows, "\n"), "┊", P.Faint, P.Muted, inner, p.indent))
	}
	if m.failure != nil {
		if p.failureAt < 0 {
			p.failureAt = len(p.blocks)
		}
		p.blocks = slices.Insert(p.blocks, p.failureAt, m.failureView())
	}
	if len(p.blocks) == 0 {
		return m.emptyState(inner, p.indent)
	}
	return "\n" + strings.Join(p.blocks, "\n\n") + "\n"
}

// toolTimeline pairs each tool call with its result and the moment it started:
// a call runs once the model finishes asking for it.
func toolTimeline(messages []message.Message) (map[string]message.ToolResult, map[string]int64) {
	results, started := map[string]message.ToolResult{}, map[string]int64{}
	for _, msg := range messages {
		for _, result := range msg.ToolResults() {
			results[result.ToolCallID] = result
		}
		for _, call := range msg.ToolCalls() {
			started[call.ID] = msg.CreatedAt
			if finish := msg.FinishPart(); finish != nil {
				started[call.ID] = finish.Time
			}
		}
	}
	return results, started
}

// lastReplies names the reply each turn shows: only its last public text.
func lastReplies(messages []message.Message) map[int]string {
	latest, turn := map[int]string{}, 0
	for _, msg := range messages {
		if msg.Role == message.User && !msg.IsApplication() {
			turn++
		}
		if msg.Role == message.Assistant && strings.TrimSpace(msg.Content().String()) != "" {
			latest[turn] = msg.ID
		}
	}
	return latest
}

// transcriptPaint is one pass over the conversation, block by block.
type transcriptPaint struct {
	m               *Model
	inner           int
	indent          string
	results         map[string]message.ToolResult
	started         map[string]int64
	commands        []commandEntry
	commandByID     map[string]commandEntry
	fresh           map[string]renderedBlock
	blocks          []string
	steps, turnCall []message.ToolCall
	failureAt       int // the recovery card follows the last steps of the conversation
}

func (p *transcriptPaint) conversation() {
	latest, turn := lastReplies(p.m.messages), 0
	for _, msg := range p.m.messages {
		if msg.IsApplication() || msg.Role == message.Tool {
			continue
		}
		text := strings.TrimSpace(msg.Content().String())
		if msg.Role == message.User {
			turn++
			p.turnCall = nil
			p.flushSteps()
			p.blocks = append(p.blocks, p.m.cached(msg.ID, text, p.inner, p.fresh, func() string {
				return barred(text, "┃", P.User, P.Strong, p.inner, p.indent)
			}))
			continue
		}
		if text != "" && latest[turn] == msg.ID {
			p.flushSteps()
			p.reply(msg, text)
		}
		p.calls(msg)
	}
	p.flushSteps()
}

func (p *transcriptPaint) reply(msg message.Message, text string) {
	key := text
	read := agent.SourcesLabel(agent.SourcesRead(p.turnCall, p.results))
	if msg.IsFinished() {
		key += "\x00" + string(msg.Model) + "\x00" + read // the meta line appears when it finishes
	}
	p.blocks = append(p.blocks, p.m.cached(msg.ID, key, p.inner, p.fresh, func() string {
		var body string
		if msg.IsFinished() {
			body = styles.RenderMarkdownBlocks(text, p.inner, p.inner)
		} else {
			// Settled paragraphs of the live reply come from its block cache.
			body = p.m.live.RenderStreaming(text, p.inner, p.inner)
		}
		body = strings.TrimSuffix(body, "\n")
		if finish := msg.FinishPart(); finish != nil && msg.IsFinished() {
			body += "\n" + on(P.Ground, P.Faint).Render(replyMeta(msg, finish, text, read))
		}
		return barredRendered(body, "│", P.Ask, p.indent)
	}))
}

// replyMeta is the faint line under a finished reply.
func replyMeta(msg message.Message, finish *message.Finish, text, read string) string {
	meta := " " + modelName(msg.Model) + " · " + duration(msg.CreatedAt, finish.Time)
	if read != "" {
		return meta + " · " + textFor("lido: ", "read: ") + read
	}
	if cites := agent.SourceCitations(text); len(cites) > 0 && cites[0].StartLine > 0 {
		meta += fmt.Sprintf(" · %s:%d", filepath.Base(cites[0].Path), cites[0].StartLine)
	}
	return meta
}

// calls queues a reply's tool calls as steps; answered questions show at once.
func (p *transcriptPaint) calls(msg message.Message) {
	for _, call := range msg.ToolCalls() {
		decisions := transcript.AnsweredQuestions(call, p.results[call.ID])
		if len(decisions) == 0 {
			p.steps = append(p.steps, call)
			continue
		}
		p.flushSteps()
		for _, decision := range decisions {
			text := textFor("Pergunta: ", "Question: ") + decision.Question + "\n" + textFor("Sua resposta: ", "Your answer: ") + decision.Answer
			p.blocks = append(p.blocks, barred(safeCommandText(text), "│", P.Ask, P.Text, p.inner, p.indent))
		}
	}
	p.turnCall = append(p.turnCall, msg.ToolCalls()...)
}

// stepsKey changes only when a call starts, ends or its seconds advance, so a
// run of steps is not redrawn otherwise.
func (p *transcriptPaint) stepsKey() string {
	var key strings.Builder
	fmt.Fprintf(&key, "%t:%t:%d", p.m.approval != nil, config.Get() != nil && config.Get().Preferences.ProcessVisible(), p.m.processVersion)
	for _, call := range p.steps {
		result, done := p.results[call.ID]
		fmt.Fprintf(&key, "|%s:%d:%t:%t:%t", call.ID, len(call.Input), call.Finished, done, result.IsError)
		entry, command := p.commandByID[call.ID]
		if command {
			fmt.Fprintf(&key, ":%q:%q:%q", entry.command, result.Content, result.Metadata)
		}
		if !done || command && entry.state == "running" {
			fmt.Fprintf(&key, ":%d", time.Now().Unix()) // its seconds count
		}
	}
	return key.String()
}

func (p *transcriptPaint) flushSteps() {
	if len(p.steps) == 0 {
		return
	}
	run := p.steps
	p.blocks = append(p.blocks, p.m.cached("steps:"+run[0].ID, p.stepsKey(), p.inner, p.fresh, func() string {
		return p.stepRows(run)
	}))
	p.failureAt = len(p.blocks)
	p.steps = nil
}

// stepRows draws commands on their own and folds the steps between them.
func (p *transcriptPaint) stepRows(run []message.ToolCall) string {
	waiting := p.m.approval != nil
	var rows []string
	var regular []message.ToolCall
	flushRegular := func() {
		rows = append(rows, stepsRows(regular, p.results, p.started, p.inner, waiting, P.Ground)...)
		regular = nil
	}
	for _, call := range run {
		if entry, ok := p.commandByID[call.ID]; ok {
			flushRegular()
			rows = append(rows, commandRows(entry, p.inner, waiting)...)
		} else {
			regular = append(regular, call)
		}
	}
	flushRegular()
	return p.indented(rows)
}

func (p *transcriptPaint) indented(rows []string) string {
	for i := range rows {
		rows[i] = p.indent + rows[i]
	}
	return strings.Join(rows, "\n")
}

// managedCommands shows checks and other executions with no bash call of their own.
func (p *transcriptPaint) managedCommands() {
	for _, entry := range p.commands {
		if strings.HasPrefix(entry.id, "process:") {
			p.blocks = append(p.blocks, p.indented(commandRows(entry, p.inner, false)))
		}
	}
}

// renderedBlock is a message as drawn at one width, kept while its key
// (its text, and for a reply whether it finished) stays the same.
type renderedBlock struct {
	key   string
	width int
	block string
}

func (m *Model) cached(id, key string, width int, fresh map[string]renderedBlock, draw func() string) string {
	if cached, ok := m.blocks[id]; ok && cached.key == key && cached.width == width {
		fresh[id] = cached
		return cached.block
	}
	block := draw()
	fresh[id] = renderedBlock{key, width, block}
	return block
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
// A running step counts its seconds from started (unix time per call ID).
func stepsRows(calls []message.ToolCall, results map[string]message.ToolResult, started map[string]int64, width int, waiting bool, bg color.Color) []string {
	runs := foldSteps(calls, results, started)
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
		rows = append(rows, r.row(width, waiting, bg))
	}
	return rows
}

// stepRun is consecutive calls of one tool with one outcome.
type stepRun struct {
	name, state    string
	targets        []string
	added, removed int
	since          int64
}

func foldSteps(calls []message.ToolCall, results map[string]message.ToolResult, started map[string]int64) []stepRun {
	unfold := config.Get() != nil && config.Get().Preferences.ProcessVisible() // one line per call
	var runs []stepRun
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
		runs = append(runs, stepRun{call.Name, state, []string{target}, added, removed, started[call.ID]})
	}
	return runs
}

// label names the tool and targets: the same target repeated reads "×3",
// different ones are listed once each.
func (r stepRun) label() string {
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
	return label
}

func (r stepRun) row(width int, waiting bool, bg color.Color) string {
	icon, tint, status := "✓", P.Success, ""
	switch r.state {
	case "running":
		icon, tint = "●", P.Plan
		if waiting {
			status = on(bg, P.Muted).Render(config.Text(" · aguardando sua aprovação", " · waiting for your approval"))
		} else if r.since > 0 {
			status = on(bg, P.Muted).Render(fmt.Sprintf(" · %ds", max(0, time.Now().Unix()-r.since)))
		}
	case "failed":
		icon, tint = "✗", P.Danger
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
	label := ansi.Truncate(r.label(), max(1, width-4-ansi.StringWidth(ansi.Strip(status))), "…")
	return on(bg, P.Muted).Render("› ") + on(bg, tint).Render(icon) + on(bg, P.Muted).Render(" "+label) + status
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

// composerRows: the frame's two edges and a blank row on each side of the text;
// the half-block edges leave too little air without them.
// composerRows is 0 while the agent asks: its card is where you answer.
func (m *Model) composerRows() int {
	if m.question != nil && !m.answeringLater() {
		return 0
	}
	return m.composer.Height() + 4
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
	newline := "Alt+Enter" // Shift+Enter reads as Enter unless the terminal says otherwise
	if m.shiftEnter {
		newline = "Shift+Enter"
	}
	legend := config.Text("Shift+Tab modo · / comandos · @ arquivos · "+newline+" nova linha", "Shift+Tab mode · / commands · @ files · "+newline+" new line")
	if m.busy {
		legend = config.Text("Mensagens entram na fila · Esc cancela", "Messages queue up · Esc cancels")
	}
	if len(m.mention) > 0 {
		legend = config.Text("↑↓ escolher · Tab inserir · Esc fechar", "↑↓ choose · Tab insert · Esc close")
	}
	return Box{Title: title, Legend: legend, Width: m.width, PadX: 2, PadY: 1, Accent: accent, Background: P.Ground, TitleColor: accent}.Render(strings.Join(lines, "\n"))
}

// footer: approval on the left, the run's activity, then context and model.
func (m *Model) footer() string {
	muted := on(P.Ground, P.Muted)
	label := approvalName(m.policy) + " ▾"
	compact := m.width < 80 // the proposal's 64-column screen abbreviates
	if compact {
		label = config.Text("A:pedir", "A:ask")
		switch m.policy {
		case permission.ApprovalAuto:
			label = "A:auto"
		case permission.ApprovalFull:
			label = config.Text("A:total", "A:full")
		}
	}
	tone := P.Text
	if m.policy == permission.ApprovalFull {
		tone = P.Danger
	}
	approval := on(P.Ground, tone).Render(" " + label)
	if mode := m.footerMode(); mode != "" {
		approval = on(P.Ground, ModeColor(m.mode)).Bold(true).Render(" "+mode) + approval
	}
	activity := ""
	switch {
	case m.selection != nil && m.selection.moved:
		label := textFor("Ctrl+C copiar · Esc limpar seleção", "Ctrl+C copy · Esc clear selection")
		if m.selection.notice != "" {
			label = m.selection.notice + " · Esc"
		}
		activity = on(P.Ground, P.Ask).Render("  " + label)
	case m.answeringLater():
		activity = on(P.Ground, P.Ask).Render("  " + config.Text("Pergunta pendente · Enter responde", "Question pending · Enter answers"))
	case m.question != nil:
		activity = on(P.Ground, P.Ask).Render("  " + config.Text("Aguardando sua resposta", "Waiting for your answer"))
	case m.approval != nil:
		activity = on(P.Ground, P.Plan).Render("  " + config.Text("Aguardando sua aprovação", "Waiting for your approval"))
	case m.busy:
		now := m.activity
		if now == config.Text("Trabalhando", "Working") {
			switch _, live, waiting := m.rate(); {
			case waiting:
				now = config.Text("Aguardando modelo", "Awaiting model")
			case live:
				now = config.Text("Gerando resposta", "Generating response")
			}
		}
		if m.retry != "" {
			now = m.retry
		}
		elapsed := time.Since(m.started).Truncate(time.Second)
		activity = on(P.Ground, P.Plan).Render(fmt.Sprintf(config.Text("  %s · %s · Esc cancela", "  %s · %s · Esc cancels"), now, elapsed))
		if compact {
			verb, _, _ := strings.Cut(now, " ")
			activity = on(P.Ground, P.Plan).Render(fmt.Sprintf("  %s %s", verb, elapsed))
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
	}
	// As in the classic footer: ~N while the reply streams, the last reply's
	// rate when idle.
	if speed, live, _ := m.rate(); speed > 0 && !compact {
		label := fmt.Sprintf("%d t/s", int(speed+0.5))
		if live {
			label = "~" + label
		}
		if right != "" {
			label = " · " + label
		}
		right += muted.Render(label)
	}
	if right != "" {
		right += muted.Render("   ")
	}
	name := modelName(model.ID)
	if compact {
		name = ansi.Truncate(name, 9, "…")
	}
	right += on(P.Ground, P.Text).Render(name+" ▾") + " "
	return "\n" + Spread(approval+activity, right, m.width, P.Ground)
}

// footerMode is the mode's name when the composer does not show it: while
// an approval keeps only your draft, or a question card replaces it.
func (m *Model) footerMode() string {
	if m.composerRows() > 0 && m.approval == nil {
		return ""
	}
	if m.width < 80 {
		return modeLabel(m.mode)[:1] + " "
	}
	return modeLabel(m.mode) + " "
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

// rate is the model's output speed in tokens per second. While a reply
// streams it is measured live; waiting means the model has not answered yet.
// Idle, it is the last reply's.
func (m *Model) rate() (speed float64, live, waiting bool) {
	if m.app == nil || m.app.CoderAgent == nil || m.session.ID == "" {
		return 0, false, false
	}
	usage, ok := m.app.CoderAgent.(agent.UsageControl)
	if !ok {
		return 0, false, false
	}
	metrics, err := usage.Usage(context.Background(), m.session.ID)
	if err != nil {
		return 0, false, false
	}
	if m.busy && metrics.StreamActive {
		waiting = metrics.StreamOutputBytes == 0
		// The first burst can hold buffered output; measure after it.
		if metrics.StreamSamples >= 2 && metrics.StreamSampleDurationNS >= int64(250*time.Millisecond) {
			speed = metrics.StreamSampleTokens * 1e9 / float64(metrics.StreamSampleDurationNS)
		}
		return speed, !waiting, waiting
	}
	if !m.busy && metrics.LastOutputTokens > 0 && metrics.LastOutputDurationNS > 0 {
		speed = float64(metrics.LastOutputTokens) * 1e9 / float64(metrics.LastOutputDurationNS)
	}
	return speed, false, false
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
