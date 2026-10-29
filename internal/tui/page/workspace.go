package page

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/raulaguila/opcode/internal/tui/commands"
	"github.com/raulaguila/opcode/internal/workflow"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/clipboard"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diff"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/review"
	"github.com/raulaguila/opcode/internal/transcript"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type WorkspaceCommandMsg struct{ Command, Value string }
type AgentOutcomeMsg struct {
	SessionID string
	Error     error
	Done      bool
	Outcome   string
}
type WorkspaceResultMsg struct {
	session string
	seq     uint64
	body    string
	details string
	err     error
	applied bool
}
type workspaceAction struct {
	label, kind, text, path string
	line                    int
}
type workspacePanel struct {
	itemEditor              *dialog.ItemEditor
	itemValues              map[int][]string
	itemText                map[int]string
	fields                  []textinput.Model
	focus                   int
	contractKind            string
	kind, title, body, note string
	details                 string
	showDetails             bool
	allActions              bool
	diffMode                string
	selected                int
	actions                 []workspaceAction
	view                    viewport.Model
	preview                 *review.Preview
	turnID                  string
	seq                     uint64
	loading                 bool
	cancel                  context.CancelFunc
}
type recoveryState struct {
	err      error
	resolved bool
}

type TerminalClipboardMsg struct {
	Sequence string
	Session  string
	Request  uint64
}

func (m TerminalClipboardMsg) Result(err error) WorkspaceResultMsg {
	return WorkspaceResultMsg{session: m.Session, seq: m.Request, err: err, body: config.Text("Cópia enviada ao terminal; recebimento não confirmado. Selecionar texto continua disponível.", "Copy sent to terminal; receipt unconfirmed. Text selection remains available.")}
}

func (p *chatPage) WorkspaceFocused() bool { return p.workspace.kind != "" }
func (p *chatPage) closeWorkspace() {
	p.workspaceHistory = nil
	p.resetWorkspace()
}
func (p *chatPage) resetWorkspace() {
	if p.workspace.cancel != nil {
		p.workspace.cancel()
	}
	seq := p.workspace.seq + 1
	p.workspace = workspacePanel{seq: seq}
}
func (p *chatPage) showWorkspace(kind, title, body string) {
	p.resetWorkspace()
	p.workspace.kind, p.workspace.title, p.workspace.body = kind, safePanel(title), safePanel(body)
	p.workspace.view = viewport.New(max(1, p.width-4), max(1, p.height-6))
	p.workspace.view.SetContent(p.workspace.body)
}
func (p *chatPage) workspaceAsync(run func(context.Context) (string, bool, error)) tea.Cmd {
	if p.workspace.cancel != nil {
		p.workspace.cancel()
	}
	p.workspace.seq++
	seq := p.workspace.seq
	id := p.session.ID
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	p.workspace.cancel = cancel
	p.workspace.loading = true
	return func() tea.Msg {
		defer cancel()
		body, applied, err := run(ctx)
		return WorkspaceResultMsg{session: id, seq: seq, body: body, err: err, applied: applied}
	}
}
func (p *chatPage) workspaceResult(msg WorkspaceResultMsg) tea.Cmd {
	if msg.session != p.session.ID || msg.seq != p.workspace.seq {
		return nil
	}
	p.workspace.loading = false
	p.workspace.cancel = nil
	if msg.err != nil {
		p.workspace.note = safePanel(msg.err.Error())
		return util.ReportError(msg.err)
	}
	p.workspace.note = ""
	p.workspace.details = safePanel(msg.details)
	if msg.body != "" {
		p.workspace.body = safePanel(msg.body)
		p.workspace.view.SetContent(p.workspace.body)
		p.workspace.view.GotoTop()
	}
	if msg.applied {
		p.refreshWorkflow()
		p.workspace.preview = nil
		return tea.Batch(util.ReportInfo(config.Text("Revisão aplicada; verificações anteriores invalidadas", "Review applied; previous checks invalidated")), p.resizeContent())
	}
	return nil
}
func (p *chatPage) workspaceCommand(command, value string) tea.Cmd {
	switch command {
	case "task":
		return p.openTask()
	case "cancel":
		if p.app != nil && p.app.CoderAgent != nil {
			p.app.CoderAgent.Cancel(p.session.ID)
		}
		return nil
	case "new":
		return p.newConversation()
	case "files":
		return util.CmdHandler(OpenFilesMsg{})
	case "commands":
		return util.CmdHandler(OpenCommandsMsg{})
	case "terminal":
		p.closeWorkspace()
		return p.toggleTerminal()
	case "worktrees":
		return p.openGitWorkspace()
	case "setup":
		return p.openSetup()
	case "local-status":
		return p.openLocalStatus()
	case "contract":
		return p.openContract()
	case "settings", "providers":
		return util.CmdHandler(dialog.OpenProviderDialogMsg{})
	case "integrations":
		return util.CmdHandler(dialog.OpenIntegrationDialogMsg{})
	case "models":
		return util.CmdHandler(openModelsMsg{})
	case "sessions":
		return util.CmdHandler(openSessionsMsg{})
	case "process":
		updated, cmd := p.messages.Update(chat.ToggleProcessMsg{})
		p.messages = updated.(layout.Container)
		return cmd
	case "latest":
		updated, cmd := p.messages.Update(chat.JumpToLatestMsg{})
		p.messages = updated.(layout.Container)
		return cmd
	case "queue":
		return p.openQueue()
	case "runtime":
		return p.openRuntime()
	case "index":
		return p.openIndex(value)
	case "context":
		return p.openContext(false)
	case "compact":
		return p.openContext(true)
	case "recalibrate":
		return p.recalibrateContext()
	case "skills":
		return p.openSkills(value)
	case "skill":
		return p.invokeSkill(value)
	case "changes", "checks":
		return p.openChanges()
	case "undo":
		return p.previewReversal(false)
	case "redo":
		return p.previewReversal(true)
	case "actions":
		text := ""
		if p.transcript != nil {
			text = p.transcript.LatestResponse()
		}
		return p.openResponseActions(text)
	case "export":
		return p.exportConversation()
	case "permissions":
		return p.openPermissions()
	case "error":
		p.showWorkspace("error", config.Text("Detalhes da execução", "Execution details"), fmt.Sprint(p.recovery.err))
		return nil
	case "retry":
		return p.retryRequest("/retry")
	}
	return nil
}

// Messages are handled by the root, avoiding a page→root import cycle.
type OpenModelsMsg struct{}
type OpenSessionsMsg struct{}
type OpenFilesMsg struct{}
type OpenCommandsMsg struct{}
type openModelsMsg = OpenModelsMsg
type openSessionsMsg = OpenSessionsMsg

func (p *chatPage) openQueue() tea.Cmd {
	p.showWorkspace("queue", config.Text("Instruções pendentes", "Pending instructions"), "")
	control, ok := p.app.CoderAgent.(agent.QueueControl)
	if !ok {
		p.workspace.body = config.Text("Fila indisponível neste agente", "Queue unavailable for this agent")
		return nil
	}
	for _, item := range control.QueuedInstructions(p.session.ID) {
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: panelText(item.Content), kind: "queue", text: item.ID})
	}
	p.workspace.body = config.Text("As instruções entram entre operações. Enter retira para editar; Delete remove. O rascunho atual é preservado.", "Instructions apply between operations. Enter takes one for editing; Delete removes it. The current draft is preserved.")
	return nil
}
func (p *chatPage) takeQueue(edit bool) tea.Cmd {
	if len(p.workspace.actions) == 0 {
		return nil
	}
	if edit {
		if draft, ok := p.composer.(chat.DraftControl); ok {
			text, attachments := draft.Draft()
			if text != "" || len(attachments) > 0 {
				p.workspace.note = config.Text("Limpe ou envie seu rascunho antes de editar a instrução", "Clear or send your draft before editing an instruction")
				return nil
			}
		}
	}
	control, ok := p.app.CoderAgent.(agent.QueueControl)
	if !ok {
		return nil
	}
	item, err := control.TakeInstruction(p.session.ID, p.workspace.actions[p.workspace.selected].text)
	if err != nil {
		p.openQueue()
		return util.ReportError(err)
	}
	if !edit {
		return p.openQueue()
	}
	p.closeWorkspace()
	updated, cmd := p.editor.Update(chat.RestoreDraftMsg{Text: item.Content, Attachments: item.Attachments})
	p.editor = updated.(layout.Container)
	return cmd
}
func (p *chatPage) openContext(compact bool) tea.Cmd {
	p.showWorkspace("context", config.Text("Contexto · orçamento", "Context · budget"), "")
	usage := fmt.Sprintf(config.Text("Uso acumulado da conversa\nEntrada: %d tokens · saída: %d tokens\nCusto estimado: $%.4f\n\n", "Conversation totals\nInput: %d tokens · output: %d tokens\nEstimated cost: $%.4f\n\n"), p.session.PromptTokens, p.session.CompletionTokens, p.session.Cost)
	control, ok := p.app.CoderAgent.(agent.ContextControl)
	if !ok {
		p.workspace.body = usage + config.Text("Este agente não expõe o orçamento de contexto", "This agent does not expose its context budget")
		return nil
	}
	id := p.session.ID
	return p.workspaceDetailedAsync(func(ctx context.Context) (string, string, error) {
		status, err := control.ContextStatus(ctx, id)
		if compact {
			status, err = control.CompactContext(ctx, id)
		}
		if err != nil {
			return "", "", err
		}
		summary := contextSummary(status)
		details, _ := json.MarshalIndent(status, "", "  ")
		return summary + "\n\n" + usage + p.contextReceipts(ctx, id), string(details), nil
	})
}

func (p *chatPage) openSkills(query ...string) tea.Cmd {
	p.showWorkspace("skills", config.Text("Skills disponíveis", "Available skills"), "")
	control, ok := p.app.CoderAgent.(agent.ExtensionControl)
	if !ok {
		return nil
	}
	skills, err := control.Skills(context.Background())
	if err != nil {
		return util.ReportError(err)
	}
	for _, skill := range skills {
		if len(query) > 0 && !commands.Match(query[0], skill.Name, skill.Description) {
			continue
		}
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: skill.Name + " · " + skill.Description, kind: "skill", text: skill.Name})
	}
	p.workspace.body = config.Text("Enter prepara /skill NOME no rascunho. As skills vêm dos diretórios explícitos da configuração global e não concedem permissões. Hooks before_tool/after_tool são opt-in no JSON global; rodam apenas em Executar com as permissões e sandbox normais.", "Enter prepares /skill NAME in the draft. Skills come from explicitly configured global directories and do not grant permissions. Hooks before_tool/after_tool are opt-in in global JSON; they run only in Execute with normal permissions and sandboxing.")
	return nil
}
func (p *chatPage) invokeSkill(value string, draft ...string) tea.Cmd {
	ackText := "/skill " + value
	if len(draft) > 0 {
		ackText = draft[0]
	}
	name, args, _ := strings.Cut(strings.TrimSpace(value), " ")
	control, ok := p.app.CoderAgent.(agent.ExtensionControl)
	if !ok || name == "" {
		err := fmt.Errorf("use /skills para escolher uma skill cadastrada")
		return tea.Batch(util.ReportError(err), util.CmdHandler(chat.SubmissionResultMsg{Text: ackText, Error: err}))
	}
	_, queued, err := control.InvokeSkill(context.Background(), p.session.ID, name, args)
	if err != nil {
		return tea.Batch(util.ReportError(err), util.CmdHandler(chat.SubmissionResultMsg{Text: ackText, Error: err}))
	}
	p.lastRequest = chat.SendMsg{Text: "/skill " + value}
	if queued {
		return tea.Batch(util.ReportInfo(config.Text("Skill adicionada à fila", "Skill queued")), util.CmdHandler(chat.SubmissionResultMsg{Text: ackText}))
	}
	return util.CmdHandler(chat.SubmissionResultMsg{Text: ackText})
}
func (p *chatPage) reviewStore() *review.Store {
	cfg := config.Get()
	return review.New(cfg.WorkingDir, cfg.Data.Directory)
}
func (p *chatPage) openChanges() tea.Cmd {
	p.showWorkspace("changes", config.Text("Alterações e verificações", "Changes and checks"), "")
	turns, err := p.reviewStore().List(p.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		state := "aplicado"
		if turn.Undone {
			state = "desfeito"
		}
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: fmt.Sprintf("%s · %d arquivos · %s", turn.ID, len(turn.Changes), state), kind: "turn", text: turn.ID})
	}
	base := config.Text("Snapshots das edições das ferramentas. Comandos, serviços externos e alterações manuais não são desfeitos.\n\n", "Snapshots of tool edits. Commands, external services and manual changes are not undone.\n\n")
	p.workspace.body = base
	if p.app.Workflows == nil {
		return nil
	}
	store, perms, id := p.app.Workflows, p.app.Permissions, p.session.ID
	return p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
		ctx = tools.WithWorkflowStore(ctx, store)
		ctx = context.WithValue(ctx, tools.SessionIDContextKey, id)
		tool := tools.NewValidationTools()[1]
		result, err := tools.SecureTool(tool, perms).Run(ctx, tools.ToolCall{Name: "get_validation_report", Input: "{}"})
		if err != nil {
			return "", false, err
		}
		if result.IsError {
			return "", false, errors.New(result.Content)
		}
		report, err := formatValidationReport(result.Content)
		return base + report, false, err
	})
}
func formatValidationReport(body string) (string, error) {
	var report struct {
		Revision   int64              `json:"task_revision"`
		Digest     string             `json:"task_digest"`
		Configured map[string]string  `json:"configured"`
		Receipts   []workflow.Receipt `json:"receipts"`
		Problem    string             `json:"validation_problem"`
	}
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		return "", fmt.Errorf("%s", config.Text("relatório de validação inválido", "invalid validation report"))
	}
	var out strings.Builder
	fmt.Fprintf(&out, config.Text("Revisão atual: %d · estado conferido ao abrir este painel\n", "Current revision: %d · state checked when opening this panel\n"), report.Revision)
	for _, receipt := range report.Receipts {
		command := report.Configured[receipt.Name]
		current := receipt.Passed && receipt.Revision == report.Revision && report.Digest != "" && receipt.Digest == report.Digest && command != "" && fmt.Sprintf("%x", sha256.Sum256([]byte(command))) == receipt.Command
		fmt.Fprintf(&out, config.Text("%s · aprovação atual: %t · resultado registrado: %t · revisão %d\n", "%s · current approval: %t · recorded result: %t · revision %d\n"), receipt.Name, current, receipt.Passed, receipt.Revision)
	}
	if len(report.Receipts) == 0 {
		out.WriteString(config.Text("Nenhuma verificação executada.\n", "No checks executed.\n"))
	}
	if report.Problem != "" {
		fmt.Fprintf(&out, "\nPendente: %s\n", report.Problem)
	}
	return out.String(), nil
}

func (p *chatPage) showTurn(id string) tea.Cmd {
	turns, err := p.reviewStore().List(p.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	var b strings.Builder
	var files []workspaceAction
	for _, t := range turns {
		if t.ID != id {
			continue
		}
		for _, c := range t.Changes {
			before, after := "", ""
			if c.Before != nil {
				before = *c.Before
			}
			if c.After != nil {
				after = *c.After
			}
			text, _, _ := diff.GenerateDiff(before, after, c.Path)
			files = append(files, workspaceAction{label: c.Path, kind: "diff-file", path: c.Path})
			b.WriteString(text)
			b.WriteByte('\n')
		}
	}
	p.showWorkspace("turn", config.Text("Alterações da interação", "Turn changes"), b.String())
	p.workspace.turnID = id
	p.workspace.actions = files
	return nil
}
func (p *chatPage) previewReversal(redo bool) tea.Cmd {
	if p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy() {
		return util.ReportWarn(config.Text("Aguarde ou cancele a execução antes de reverter arquivos", "Wait or cancel execution before reverting files"))
	}
	turnID := ""
	if p.workspace.kind == "turn" {
		turnID = p.workspace.turnID
	}
	preview, err := p.reviewStore().Preview(p.session.ID, turnID, redo)
	if err != nil {
		return util.ReportError(err)
	}
	title := config.Text("Desfazer interação", "Undo turn")
	if redo {
		title = config.Text("Refazer interação", "Redo turn")
	}
	p.showWorkspace("review", title, preview.Diff)
	p.workspace.preview = &preview
	p.workspace.note = config.Text("Ctrl+Enter solicita aprovação dos diffs. Esc cancela.", "Ctrl+Enter requests diff approval. Esc cancels.")
	return nil
}
func (p *chatPage) applyReversal() tea.Cmd {
	if p.workspace.preview == nil || (p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy()) {
		return util.ReportWarn(config.Text("Revisão indisponível durante execução", "Review unavailable during execution"))
	}
	store := p.reviewStore()
	preview := *p.workspace.preview
	id := p.session.ID
	perms := p.app.Permissions
	workflow := p.app.Workflows
	return p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
		var changed func() error
		if workflow != nil {
			changed = func() error { return workflow.Mutated(ctx, id) }
		}
		err := store.Apply(ctx, id, preview, perms, changed)
		return config.Text("Revisão aplicada. Confira os arquivos e execute novamente as verificações necessárias.", "Review applied. Inspect the files and rerun required checks."), err == nil, err
	})
}
func (p *chatPage) openPermissions() tea.Cmd {
	p.showWorkspace("permissions", config.Text("Permissões desta sessão", "Session permissions"), "")
	var b strings.Builder
	if inspector, ok := p.app.Permissions.(interface {
		SessionGrants(string) []permission.PermissionRequest
	}); ok {
		for _, grant := range inspector.SessionGrants(p.session.ID) {
			fmt.Fprintf(&b, "%s · %s\n%s\n\n", grant.ToolName, grant.Action, grant.Path)
		}
	}
	if b.Len() == 0 {
		b.WriteString(config.Text("Nenhuma autorização lembrada nesta sessão.\n", "No remembered approvals in this session.\n"))
	}
	b.WriteString(config.Text("\nR revoga as autorizações lembradas e solicitações pendentes. Isso não desfaz arquivos nem restaura aprovações de sessões anteriores.", "\nR revokes remembered approvals and pending requests. It does not undo files or restore approvals from past sessions."))
	p.workspace.body = b.String()
	return nil
}
func (p *chatPage) exportConversation() tea.Cmd {
	if p.session.ID == "" {
		return util.ReportWarn("Abra uma conversa antes de exportar")
	}
	msgs, err := p.app.Messages.List(context.Background(), p.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	data, err := transcript.Markdown(p.session, msgs, tools.RedactCredentials)
	if err != nil {
		return util.ReportError(err)
	}
	cfg := config.Get()
	dir := cfg.Data.Directory
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cfg.WorkingDir, dir)
	}
	path := filepath.Join(dir, "exports", safeExportName(p.session.ID)+"-"+time.Now().Format("20060102-150405.000000000")+".md")
	if err = localstate.WriteBytes(path, data); err != nil {
		return util.ReportError(err)
	}
	return util.ReportInfo("Conversa exportada: " + path)
}
func safeExportName(id string) string {
	return regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(id, "_")
}
func (p *chatPage) openResponseActions(text string) tea.Cmd {
	if text == "" {
		return util.ReportWarn(config.Text("Nenhuma resposta disponível", "No response available"))
	}
	text = tools.RedactCredentials(text)
	p.showWorkspace("actions", config.Text("Ações da resposta", "Response actions"), "")
	p.workspace.actions = []workspaceAction{{label: config.Text("Copiar resposta", "Copy response"), kind: "copy", text: text}, {label: config.Text("Selecionar texto da resposta", "Select response text"), kind: "text", text: text}}
	for i, match := range regexp.MustCompile("(?s)```[^\\n]*\\n(.*?)```").FindAllStringSubmatch(text, 10) {
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: fmt.Sprintf(config.Text("Copiar código %d", "Copy code %d"), i+1), kind: "copy", text: match[1]})
	}
	seen := map[string]bool{}
	for _, match := range regexp.MustCompile("`([^`\\n]+)`|([[:alnum:]_./-]+\\.[[:alnum:]]+:[0-9]+)").FindAllStringSubmatch(text, 40) {
		path := match[1]
		if path == "" {
			path = match[2]
		}
		line := 1
		if ix := strings.LastIndex(path, ":"); ix > 0 {
			if _, err := fmt.Sscanf(path[ix+1:], "%d", &line); err == nil {
				path = path[:ix]
			}
		}
		if strings.ContainsAny(path, "\n\x00") || strings.Contains(path, "://") || seen[path] {
			continue
		}
		absolute := path
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(config.WorkingDirectory(), path)
		}
		info, err := os.Lstat(absolute)
		if err != nil || info.IsDir() {
			continue
		}
		seen[path] = true
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: config.Text("Abrir fonte · ", "Open source · ") + path, kind: "file", path: absolute, line: line})
	}
	p.workspace.body = config.Text("Escolha uma ação. Prévia de arquivo é leitura local e não libera alterações para o agente.", "Choose an action. File preview is a local read and does not authorize agent edits.")
	return nil
}
func (p *chatPage) openFile(path string, line int) tea.Cmd {
	p.showWorkspace("file", config.Text("Arquivo · ", "File · ")+filepath.Base(path), "")
	id := p.session.ID
	perms := p.app.Permissions
	return p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
		ctx = context.WithValue(ctx, tools.SessionIDContextKey, id)
		input, _ := json.Marshal(map[string]any{"file_path": path, "offset": max(0, line-1), "limit": 200})
		result, err := tools.SecureTool(tools.NewPreviewTool(), perms).Run(ctx, tools.ToolCall{Name: "file_preview", Input: string(input)})
		if err == nil && result.IsError {
			err = errors.New(result.Content)
		}
		return result.Content, false, err
	})
}
func (p *chatPage) workspaceChoose() (cmd tea.Cmd) {
	previous := p.workspace
	defer func() {
		if previous.kind != "" && p.workspace.kind != "" && previous.kind != p.workspace.kind {
			previous.cancel = nil
			previous.loading = false
			p.workspaceHistory = append(p.workspaceHistory, previous)
			if len(p.workspaceHistory) > 8 {
				p.workspaceHistory = p.workspaceHistory[len(p.workspaceHistory)-8:]
			}
		}
	}()
	if p.workspace.loading {
		return nil
	}
	if len(p.workspace.actions) == 0 {
		return nil
	}
	item := p.workspace.actions[p.workspace.selected]
	switch item.kind {
	case "adjust-plan", "execute-plan", "confirm-plan":
		return p.planAction(item.kind)
	case "command":
		return p.workspaceCommand(item.text, "")
	case "worktree-open", "worktree-open-confirm", "worktree-create", "worktree-create-confirm", "git-diff", "git-diff-staged", "git-checks":
		return p.gitChoose(item)
	case "contract-kind":
		return p.editContract(item.text)
	case "setup-details", "setup-providers", "setup-models", "setup-probe", "setup-read", "setup-done", "setup-probe-confirm", "setup-read-confirm", "local-status":
		return p.setupChoose(item.kind)
	case "diff-file":
		rendered := dialog.RenderReviewDiff(p.workspace.body, max(1, p.width-4), p.workspace.diffMode)
		for i, line := range strings.Split(ansi.Strip(rendered), "\n") {
			if strings.Contains(line, item.path) && (strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "File:") || strings.HasPrefix(line, "Arquivo:")) {
				p.workspace.view.SetYOffset(i)
				break
			}
		}
		return nil
	case "queue":
		return p.takeQueue(true)
	case "skill":
		p.closeWorkspace()
		updated, cmd := p.editor.Update(chat.RestoreDraftMsg{Text: "/skill " + item.text + " "})
		p.editor = updated.(layout.Container)
		return cmd
	case "turn":
		return p.showTurn(item.text)
	case "text":
		p.showWorkspace("text", config.Text("Texto para seleção", "Selectable text"), item.text)
		return nil
	case "file":
		return p.openFile(item.path, item.line)
	case "copy":
		text := item.text
		if clipboard.Remote() {
			sequence, err := clipboard.OSC52(text)
			if err != nil {
				return util.ReportError(err)
			}
			p.workspace.seq++
			p.workspace.loading = true
			return util.CmdHandler(TerminalClipboardMsg{Sequence: sequence, Session: p.session.ID, Request: p.workspace.seq})
		}
		p.workspace.seq++
		p.workspace.loading = true
		id, seq := p.session.ID, p.workspace.seq
		return util.CmdHandler(util.CopyTextMsg{Text: text, Result: func(confirmed bool, err error) tea.Msg {
			body := config.Text("Conteúdo copiado para a área de transferência.", "Content copied to clipboard.")
			if err != nil {
				err = fmt.Errorf(config.Text("clipboard indisponível; use Shift+arrastar ou Selecionar texto: %w", "clipboard unavailable; use Shift+drag or Select text: %w"), err)
			}
			if !confirmed {
				body = config.Text("Cópia enviada ao terminal; recebimento não confirmado. Selecionar texto continua disponível.", "Copy sent to terminal; receipt unconfirmed. Text selection remains available.")
			}
			return WorkspaceResultMsg{session: id, seq: seq, body: body, err: err}
		}})
	}
	return nil
}
func (p *chatPage) workspaceKey(msg tea.KeyMsg) tea.Cmd {
	if contextPanel(p.workspace.kind) && strings.Contains("1234", msg.String()) && len(msg.String()) == 1 {
		return p.contextTab(msg.String())
	}
	if p.workspace.kind == "contract" {
		return p.contractKey(msg)
	}
	if p.workspace.loading && msg.String() != "esc" {
		p.workspace.view, _ = p.workspace.view.Update(msg)
		return nil
	}
	switch msg.String() {
	case "m":
		p.workspace.allActions = !p.workspace.allActions
		return nil
	case "d":
		if p.workspace.details != "" {
			p.workspace.showDetails = !p.workspace.showDetails
			p.workspace.view.GotoTop()
		}
		return nil
	case "t":
		if p.workspace.kind == "turn" || p.workspace.kind == "review" {
			switch p.workspace.diffMode {
			case "unified":
				p.workspace.diffMode = "split"
			case "split":
				p.workspace.diffMode = "auto"
			default:
				p.workspace.diffMode = "unified"
			}
		}
		return nil
	case "esc":
		p.backWorkspace()
		return nil
	case "enter":
		return p.workspaceChoose()
	case "ctrl+j", "ctrl+enter":
		if p.workspace.kind == "review" {
			return p.applyReversal()
		}
	case "up":
		if len(p.workspace.actions) > 0 {
			p.workspace.selected = max(0, p.workspace.selected-1)
			return nil
		}
	case "down":
		if len(p.workspace.actions) > 0 {
			p.workspace.selected = min(len(p.workspace.actions)-1, p.workspace.selected+1)
			return nil
		}
	case "delete", "backspace":
		if p.workspace.kind == "queue" {
			return p.takeQueue(false)
		}
	case "u":
		if p.workspace.kind == "changes" || p.workspace.kind == "turn" {
			return p.previewReversal(false)
		}
	case "r":
		if p.workspace.kind == "context" {
			return p.recalibrateContext()
		}
		if p.workspace.kind == "changes" || p.workspace.kind == "turn" {
			return p.previewReversal(true)
		}
		if p.workspace.kind == "permissions" {
			p.app.Permissions.RevokeSession(p.session.ID)
			return p.openPermissions()
		}
	case "c":
		if p.workspace.kind == "context" {
			return p.openContext(true)
		}
	case "ctrl+r":
		return p.workspaceCommand(p.workspace.kind, "")
	}
	p.workspace.view, _ = p.workspace.view.Update(msg)
	return nil
}
func (p *chatPage) workspaceView() string {
	if p.workspace.kind == "contract" {
		return ansi.Hardwrap(p.contractView(), max(1, p.width-2), true)
	}
	w, h := max(1, p.width-4), max(1, p.height-6)
	t := theme.CurrentTheme()
	lines := []string{styles.BaseStyle().Foreground(t.Primary()).Bold(true).Render(ansi.Truncate(safePanel(p.workspace.title), w, "…"))}
	if len(p.workspaceHistory) > 0 {
		lines = append(lines, ansi.Truncate(safePanel(p.workspaceHistory[len(p.workspaceHistory)-1].title)+" › "+safePanel(p.workspace.title), w, "…"))
		h--
	}
	help := config.Text("↑↓/PgUp/PgDn rola · Ctrl+R atualiza · Esc volta", "↑↓/PgUp/PgDn scroll · Ctrl+R refresh · Esc back")
	if len(p.workspace.actions) > 0 {
		_, maxRows, start := p.workspaceActionGeometry()
		for i := start; i < min(len(p.workspace.actions), start+maxRows); i++ {
			style := styles.BaseStyle().Width(w)
			mark := " "
			if i == p.workspace.selected {
				style = style.Background(t.Primary()).Foreground(t.Background())
				mark = "›"
			}
			lines = append(lines, style.Render(ansi.Truncate(mark+" "+panelText(p.workspace.actions[i].label), w, "…")))
		}
		help = config.Text("↑↓ escolhe · Enter abre · Esc volta", "↑↓ choose · Enter open · Esc back")
		h -= min(maxRows, len(p.workspace.actions))
		if len(p.workspace.actions) > 3 {
			lines = append(lines, config.Text("M mais/menos ações", "M more/fewer actions"))
			h--
		}
	}
	if p.workspace.kind == "queue" {
		help = config.Text("Enter edita · Delete remove · Ctrl+R atualiza · Esc volta", "Enter edit · Delete remove · Ctrl+R refresh · Esc back")
	}
	if p.workspace.kind == "review" {
		help = config.Text("Ctrl+Enter aprova · PgUp/PgDn rola · Esc cancela", "Ctrl+Enter approve · PgUp/PgDn scroll · Esc cancel")
	}
	if p.workspace.kind == "context" {
		help = config.Text("C compacta · R recalibra · Ctrl+R atualiza · Esc volta", "C compact · R recalibrate · Ctrl+R refresh · Esc back")
	}
	if p.workspace.kind == "changes" || p.workspace.kind == "turn" {
		help = config.Text("Enter arquivo · T diff · U desfaz · R refaz · Esc volta", "Enter file · T diff · U undo · R redo · Esc back")
	}
	if p.workspace.details != "" {
		help = config.Text("D detalhes · ", "D details · ") + help
	}
	p.workspace.view.Width = w
	p.workspace.view.Height = max(1, h)
	if p.workspace.kind == "turn" || p.workspace.kind == "review" {
		p.workspace.view.SetContent(dialog.RenderReviewDiff(p.workspace.body, w, p.workspace.diffMode))
	} else {
		body := p.workspace.body
		if p.workspace.showDetails {
			body += "\n\n" + p.workspace.details
		}
		p.workspace.view.SetContent(ansi.Hardwrap(safePanel(body), w, true))
	}
	if contextPanel(p.workspace.kind) {
		lines = append(lines, ansi.Truncate(config.Text("1 Orçamento · 2 Índice · 3 Alocação · 4 Diagnóstico", "1 Budget · 2 Index · 3 Allocation · 4 Diagnostics"), w, "…"))
		p.workspace.view.Height = max(1, p.workspace.view.Height-1)
	}
	lines = append(lines, p.workspace.view.View())
	if p.workspace.loading {
		lines = append(lines, config.Text("Carregando… · Esc cancela", "Loading… · Esc cancels"))
	}
	if p.workspace.note != "" {
		lines = append(lines, ansi.Truncate(safePanel(p.workspace.note), w, "…"))
	}
	lines = append(lines, ansi.Truncate(help, w, "…"))
	return styles.BaseStyle().Padding(1, 2).Width(max(1, p.width-4)).Height(max(1, p.height-2)).Render(strings.Join(lines, "\n"))
}

func (p *chatPage) handleWorkspaceMessage(text string, attachments []message.Attachment) (tea.Cmd, bool) {
	command, value, _ := strings.Cut(strings.TrimSpace(text), " ")
	command = strings.TrimPrefix(command, "/")
	if !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return nil, false
	}
	switch command {
	case "cancel", "new", "files", "terminal", "commands", "worktrees", "setup", "local-status", "contract", "settings", "providers", "models", "integrations", "sessions", "process", "latest", "queue", "runtime", "index", "context", "compact", "recalibrate", "skills", "skill", "changes", "checks", "undo", "redo", "actions", "export", "permissions", "retry", "error":
	default:
		return nil, false
	}
	if len(attachments) > 0 {
		err := fmt.Errorf("%s", config.Text("este comando não aceita anexos; o rascunho foi preservado", "this command does not accept attachments; the draft was preserved"))
		return tea.Batch(util.ReportError(err), util.CmdHandler(chat.SubmissionResultMsg{Text: text, Error: err})), true
	}
	var cmd tea.Cmd
	if command == "skill" && p.session.ID == "" {
		sess, err := p.app.Sessions.Create(context.Background(), "Skill")
		if err != nil {
			return tea.Batch(util.ReportError(err), util.CmdHandler(chat.SubmissionResultMsg{Text: text, Error: err})), true
		}
		p.session = sess
		cmd = util.CmdHandler(chat.SessionSelectedMsg(sess))
	}
	if command == "retry" {
		return p.retryRequest(text), true
	}
	if command == "skill" {
		return tea.Batch(cmd, p.invokeSkill(strings.TrimSpace(value), text)), true
	}
	result := p.workspaceCommand(command, strings.TrimSpace(value))
	return tea.Batch(cmd, result, util.CmdHandler(chat.SubmissionResultMsg{Text: text})), true
}
func cloneUserAttachments(items []message.Attachment) []message.Attachment {
	result := append([]message.Attachment(nil), items...)
	for i := range result {
		result[i].Content = append([]byte(nil), result[i].Content...)
	}
	return result
}
func (p *chatPage) workspaceMouse(msg tea.MouseMsg) tea.Cmd {
	if p.workspace.kind == "contract" {
		if p.workspace.itemEditor != nil {
			return nil
		}
		if msg.Button == tea.MouseButtonWheelUp {
			return p.contractKey(tea.KeyMsg{Type: tea.KeyUp})
		}
		if msg.Button == tea.MouseButtonWheelDown {
			return p.contractKey(tea.KeyMsg{Type: tea.KeyDown})
		}
		count := max(1, min(len(p.workspace.fields), (p.height-7)/2))
		start := max(0, min(p.workspace.focus-count+1, len(p.workspace.fields)-count))
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.Y >= 2 && msg.Y < 2+count*2 {
			p.workspace.fields[p.workspace.focus].Blur()
			p.workspace.focus = start + (msg.Y-2)/2
			return p.workspace.fields[p.workspace.focus].Focus()
		}
		return nil
	}

	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		p.workspace.view, _ = p.workspace.view.Update(msg)
		return nil
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		first, rows, start := p.workspaceActionGeometry()
		shown := min(rows, len(p.workspace.actions))
		if len(p.workspace.actions) > 3 && msg.Y == first+shown {
			p.workspace.allActions = !p.workspace.allActions
			return nil
		}
		index := start + msg.Y - first
		if msg.Y >= first && msg.Y < first+shown && index >= 0 && index < len(p.workspace.actions) {
			p.workspace.selected = index
			return p.workspaceChoose()
		}
	}
	return nil
}

func (p *chatPage) retryRequest(draft string) tea.Cmd {
	if p.lastRequest.Text == "" {
		err := fmt.Errorf("%s", config.Text("nenhuma pergunta anterior disponível", "no previous question available"))
		return tea.Batch(util.ReportError(err), util.CmdHandler(chat.SubmissionResultMsg{Text: draft, Error: err}))
	}
	if strings.HasPrefix(p.lastRequest.Text, "/skill ") {
		return p.invokeSkill(strings.TrimPrefix(p.lastRequest.Text, "/skill "), draft)
	}
	text := p.lastRequest.Text + "\n\nRetome conferindo o progresso e o estado atual dos arquivos. Reutilize resultados existentes quando válidos e evite repetir operações já aplicadas."
	original := p.lastRequest
	cmd := p.submitAgentMessage(text, original.Attachments, draft)
	p.lastRequest = original
	return cmd
}

func safePanel(text string) string {
	text = ansi.Strip(tools.RedactCredentials(text))
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
}

func (p *chatPage) recalibrateContext() tea.Cmd {
	control, ok := p.app.CoderAgent.(agent.ContextCalibrationControl)
	if !ok {
		return util.ReportWarn(config.Text("Este agente não oferece recalibração", "This agent does not support recalibration"))
	}
	id := p.session.ID
	p.showWorkspace("context", config.Text("Recalibrar contexto", "Recalibrate context"), "")
	return p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
		status, err := control.RecalibrateContext(ctx, id)
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf(config.Text("Limite aprendido removido. Janela configurada: %d tokens. O próximo turno usará esse limite; nenhuma ferramenta foi repetida.", "Learned limit removed. Configured window: %d tokens. The next turn will use that limit; no tool was replayed."), status.ContextWindow), false, nil
	})
}

func (p *chatPage) openIndex(action string) tea.Cmd {
	if action == "" {
		action = "status"
	}
	p.showWorkspace("index", config.Text("Índice do projeto", "Project index"), "")
	return p.workspaceDetailedAsync(func(ctx context.Context) (string, string, error) {
		verb, path, _ := strings.Cut(strings.TrimSpace(action), " ")
		status, err := tools.ControlCodeIndex(ctx, verb, strings.TrimSpace(path))
		if err != nil {
			return "", "", err
		}
		state := config.Text("Ativo", "Active")
		if status.Paused {
			state = config.Text("Pausado", "Paused")
		}
		summary := fmt.Sprintf(config.Text("Índice: %s\n%d arquivos · %d trechos · %d pendentes\n\nA busca usa este índice derivado; os arquivos originais são preservados.", "Index: %s\n%d files · %d chunks · %d pending\n\nSearch uses this derived index; original files are preserved."), state, status.Files, status.Chunks, status.Pending)
		if status.Exclusion != "" {
			summary += "\n" + status.Exclusion
		}
		summary += config.Text("\n\n/index update · rebuild · pause · resume · clear\nCLI: opcode index explain CAMINHO. Nenhuma inferência é executada.", "\n\n/index update · rebuild · pause · resume · clear\nCLI: opcode index explain PATH. No inference is executed.")
		raw, err := json.MarshalIndent(status, "", "  ")
		return summary, string(raw), err
	})
}

func (p *chatPage) openRuntime() tea.Cmd {
	p.showWorkspace("runtime", config.Text("Diagnóstico da execução", "Execution diagnostics"), "")
	return p.workspaceDetailedAsync(func(ctx context.Context) (string, string, error) {
		c, ok := p.app.CoderAgent.(agent.RuntimeDiagnosticControl)
		if !ok {
			return "", "", fmt.Errorf("%s", config.Text("diagnóstico indisponível", "runtime diagnostic unavailable"))
		}
		report, err := c.RuntimeDiagnostic(ctx, p.session.ID)
		if err != nil {
			return "", "", err
		}
		raw, err := json.MarshalIndent(report, "", "  ")
		summary := fmt.Sprintf(config.Text("Modelo: %v\nProtocolo: %v\nProjeto: %v\n\nDiagnóstico da configuração atual. Credenciais omitidas; não executa inferência. D mostra os detalhes técnicos.", "Model: %v\nProtocol: %v\nProject: %v\n\nCurrent configuration diagnostics. Credentials omitted; no inference is executed. D shows technical details."), report["model"], report["protocol"], report["cwd"])
		if usage, ok := report["usage"].(agent.UsageMetrics); ok {
			summary += "\n\n" + fmt.Sprintf(config.Text("Tempos observados nesta sessão: contexto %d ms · provedor %d ms · ferramentas %d ms\nChamadas: %d · reparos: %d", "Observed session times: context %d ms · provider %d ms · tools %d ms\nCalls: %d · repairs: %d"), usage.ContextMS, usage.ProviderMS, usage.ToolsMS, usage.ProviderCalls, usage.Repairs)
		}
		return summary, string(raw), err
	})
}

func (p *chatPage) workspaceActionGeometry() (first, rows, start int) {
	first = 2
	h := max(1, p.height-6)
	if len(p.workspaceHistory) > 0 {
		first++
		h--
	}
	rows = max(1, min(10, h-4))
	if !p.workspace.allActions {
		rows = min(rows, 3)
	}
	start = max(0, min(p.workspace.selected-rows+1, len(p.workspace.actions)-rows))
	return
}
