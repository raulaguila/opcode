package page

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/raulaguila/opcode/internal/workflow"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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
}
type WorkspaceResultMsg struct {
	session string
	seq     uint64
	body    string
	err     error
	applied bool
}
type workspaceAction struct {
	label, kind, text, path string
	line                    int
}
type workspacePanel struct {
	kind, title, body, note string
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

var writeClipboard = clipboard.WriteAll

func (p *chatPage) WorkspaceFocused() bool { return p.workspace.kind != "" }
func (p *chatPage) closeWorkspace() {
	if p.workspace.cancel != nil {
		p.workspace.cancel()
	}
	seq := p.workspace.seq + 1
	p.workspace = workspacePanel{seq: seq}
}
func (p *chatPage) showWorkspace(kind, title, body string) {
	p.closeWorkspace()
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
	if msg.body != "" {
		p.workspace.body = safePanel(msg.body)
		p.workspace.view.SetContent(p.workspace.body)
		p.workspace.view.GotoTop()
	}
	if msg.applied {
		p.refreshWorkflow()
		p.workspace.preview = nil
		return tea.Batch(util.ReportInfo("Revisão aplicada; verificações anteriores invalidadas"), p.resizeContent())
	}
	return nil
}
func (p *chatPage) workspaceCommand(command, value string) tea.Cmd {
	switch command {
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
	case "context":
		return p.openContext(false)
	case "compact":
		return p.openContext(true)
	case "skills":
		return p.openSkills()
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
		p.showWorkspace("error", "Detalhes da execução", fmt.Sprint(p.recovery.err))
		return nil
	case "retry":
		return p.retryRequest("/retry")
	}
	return nil
}

// Messages are handled by the root, avoiding a page→root import cycle.
type OpenModelsMsg struct{}
type OpenSessionsMsg struct{}
type openModelsMsg = OpenModelsMsg
type openSessionsMsg = OpenSessionsMsg

func (p *chatPage) openQueue() tea.Cmd {
	p.showWorkspace("queue", "Instruções pendentes", "")
	control, ok := p.app.CoderAgent.(agent.QueueControl)
	if !ok {
		p.workspace.body = "Fila indisponível neste agente"
		return nil
	}
	for _, item := range control.QueuedInstructions(p.session.ID) {
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: panelText(item.Content), kind: "queue", text: item.ID})
	}
	p.workspace.body = "As instruções entram entre operações. Enter retira para editar; Delete remove. O rascunho atual é preservado."
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
				p.workspace.note = "Limpe ou envie seu rascunho antes de editar a instrução"
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
	p.showWorkspace("context", "Contexto do modelo", "")
	control, ok := p.app.CoderAgent.(agent.ContextControl)
	if !ok {
		p.workspace.body = "Este agente não expõe o orçamento de contexto"
		return nil
	}
	id := p.session.ID
	return p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
		status, err := control.ContextStatus(ctx, id)
		if compact {
			status, err = control.CompactContext(ctx, id)
		}
		if err != nil {
			return "", false, err
		}
		total := status.EstimatedInputTokens + status.ReservedOutputTokens + status.SafetyTokens
		text := fmt.Sprintf("Método: %s\nJanela: %d tokens\nEntrada estimada: %d\nSchemas das ferramentas: %d\nReserva de resposta: %d\nMargem: %d\nTotal previsto: %d\nCabe na janela: %t\nMensagens compactadas: %d\n\nA estimativa inclui instruções, histórico e schemas. O limite do servidor pode ser menor. Compactar altera apenas a preparação do contexto, preservando a conversa salva.", status.Method, status.ContextWindow, status.EstimatedInputTokens, status.ToolSchemaTokens, status.ReservedOutputTokens, status.SafetyTokens, total, status.Fits, status.CompactedMessages)
		return text, false, nil
	})
}
func (p *chatPage) openSkills() tea.Cmd {
	p.showWorkspace("skills", "Skills disponíveis", "")
	control, ok := p.app.CoderAgent.(agent.ExtensionControl)
	if !ok {
		return nil
	}
	skills, err := control.Skills(context.Background())
	if err != nil {
		return util.ReportError(err)
	}
	for _, skill := range skills {
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: skill.Name + " · " + skill.Description, kind: "skill", text: skill.Name})
	}
	p.workspace.body = "Enter prepara /skill NOME no rascunho. As skills vêm dos diretórios explícitos da configuração global e não concedem permissões. Hooks before_tool/after_tool são opt-in no JSON global; rodam apenas em Executar com as permissões e sandbox normais."
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
		return tea.Batch(util.ReportInfo("Skill adicionada à fila"), util.CmdHandler(chat.SubmissionResultMsg{Text: ackText}))
	}
	return util.CmdHandler(chat.SubmissionResultMsg{Text: ackText})
}
func (p *chatPage) reviewStore() *review.Store {
	cfg := config.Get()
	return review.New(cfg.WorkingDir, cfg.Data.Directory)
}
func (p *chatPage) openChanges() tea.Cmd {
	p.showWorkspace("changes", "Alterações e verificações", "")
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
	base := "Snapshots das edições das ferramentas. Comandos, serviços externos e alterações manuais não são desfeitos.\n\n"
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
		return "", fmt.Errorf("relatório de validação inválido")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Revisão atual: %d · estado conferido ao abrir este painel\n", report.Revision)
	for _, receipt := range report.Receipts {
		command := report.Configured[receipt.Name]
		current := receipt.Passed && receipt.Revision == report.Revision && report.Digest != "" && receipt.Digest == report.Digest && command != "" && fmt.Sprintf("%x", sha256.Sum256([]byte(command))) == receipt.Command
		fmt.Fprintf(&out, "%s · aprovação atual: %t · resultado registrado: %t · revisão %d\n", receipt.Name, current, receipt.Passed, receipt.Revision)
	}
	if len(report.Receipts) == 0 {
		out.WriteString("Nenhuma verificação executada.\n")
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
			b.WriteString(text)
			b.WriteByte('\n')
		}
	}
	p.showWorkspace("turn", "Alterações da interação", b.String())
	p.workspace.turnID = id
	return nil
}
func (p *chatPage) previewReversal(redo bool) tea.Cmd {
	if p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy() {
		return util.ReportWarn("Aguarde ou cancele a execução antes de reverter arquivos")
	}
	turnID := ""
	if p.workspace.kind == "turn" {
		turnID = p.workspace.turnID
	}
	preview, err := p.reviewStore().Preview(p.session.ID, turnID, redo)
	if err != nil {
		return util.ReportError(err)
	}
	title := "Desfazer interação"
	if redo {
		title = "Refazer interação"
	}
	p.showWorkspace("review", title, preview.Diff)
	p.workspace.preview = &preview
	p.workspace.note = "Ctrl+Enter solicita aprovação dos diffs. Esc cancela."
	return nil
}
func (p *chatPage) applyReversal() tea.Cmd {
	if p.workspace.preview == nil || (p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy()) {
		return util.ReportWarn("Revisão indisponível durante execução")
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
		return "Revisão aplicada. Confira os arquivos e execute novamente as verificações necessárias.", err == nil, err
	})
}
func (p *chatPage) openPermissions() tea.Cmd {
	p.showWorkspace("permissions", "Permissões desta sessão", "")
	var b strings.Builder
	if inspector, ok := p.app.Permissions.(interface {
		SessionGrants(string) []permission.PermissionRequest
	}); ok {
		for _, grant := range inspector.SessionGrants(p.session.ID) {
			fmt.Fprintf(&b, "%s · %s\n%s\n\n", grant.ToolName, grant.Action, grant.Path)
		}
	}
	if b.Len() == 0 {
		b.WriteString("Nenhuma autorização lembrada nesta sessão.\n")
	}
	b.WriteString("\nR revoga as autorizações lembradas e solicitações pendentes. Isso não desfaz arquivos nem restaura aprovações de sessões anteriores.")
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
		return util.ReportWarn("Nenhuma resposta disponível")
	}
	text = tools.RedactCredentials(text)
	p.showWorkspace("actions", "Ações da resposta", "")
	p.workspace.actions = []workspaceAction{{label: "Copiar resposta", kind: "copy", text: text}, {label: "Selecionar texto da resposta", kind: "text", text: text}}
	for i, match := range regexp.MustCompile("(?s)```[^\\n]*\\n(.*?)```").FindAllStringSubmatch(text, 10) {
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: fmt.Sprintf("Copiar código %d", i+1), kind: "copy", text: match[1]})
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
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: "Abrir arquivo · " + path, kind: "file", path: absolute, line: line})
	}
	p.workspace.body = "Escolha uma ação. Prévia de arquivo é leitura local e não libera alterações para o agente."
	return nil
}
func (p *chatPage) openFile(path string, line int) tea.Cmd {
	p.showWorkspace("file", "Arquivo · "+filepath.Base(path), "")
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
func (p *chatPage) workspaceChoose() tea.Cmd {
	if p.workspace.loading {
		return nil
	}
	if len(p.workspace.actions) == 0 {
		return nil
	}
	item := p.workspace.actions[p.workspace.selected]
	switch item.kind {
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
		p.showWorkspace("text", "Texto para seleção", item.text)
		return nil
	case "file":
		return p.openFile(item.path, item.line)
	case "copy":
		text := item.text
		return p.workspaceAsync(func(context.Context) (string, bool, error) {
			if err := writeClipboard(text); err != nil {
				return "", false, fmt.Errorf("clipboard indisponível; escolha Selecionar texto: %w", err)
			}
			return "Conteúdo copiado para a área de transferência.", false, nil
		})
	}
	return nil
}
func (p *chatPage) workspaceKey(msg tea.KeyMsg) tea.Cmd {
	if p.workspace.loading && msg.String() != "esc" {
		p.workspace.view, _ = p.workspace.view.Update(msg)
		return nil
	}
	switch msg.String() {
	case "esc":
		p.closeWorkspace()
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
	w, h := max(1, p.width-4), max(1, p.height-6)
	t := theme.CurrentTheme()
	lines := []string{styles.BaseStyle().Foreground(t.Primary()).Bold(true).Render(ansi.Truncate(safePanel(p.workspace.title), w, "…"))}
	help := "↑↓/PgUp/PgDn rola · Ctrl+R atualiza · Esc volta"
	if len(p.workspace.actions) > 0 {
		maxRows := max(1, min(10, h-4))
		start := max(0, min(p.workspace.selected-maxRows+1, len(p.workspace.actions)-maxRows))
		for i := start; i < min(len(p.workspace.actions), start+maxRows); i++ {
			style := styles.BaseStyle().Width(w)
			mark := " "
			if i == p.workspace.selected {
				style = style.Background(t.Primary()).Foreground(t.Background())
				mark = "›"
			}
			lines = append(lines, style.Render(ansi.Truncate(mark+" "+panelText(p.workspace.actions[i].label), w, "…")))
		}
		help = "↑↓ escolhe · Enter abre · Esc volta"
		h -= min(maxRows, len(p.workspace.actions))
	}
	if p.workspace.kind == "queue" {
		help = "Enter edita · Delete remove · Ctrl+R atualiza · Esc volta"
	}
	if p.workspace.kind == "review" {
		help = "Ctrl+Enter aprova · PgUp/PgDn rola · Esc cancela"
	}
	if p.workspace.kind == "context" {
		help = "C compacta · Ctrl+R atualiza · Esc volta"
	}
	if p.workspace.kind == "changes" || p.workspace.kind == "turn" {
		help = "Enter detalhes · U desfaz · R refaz · Esc volta"
	}
	p.workspace.view.Width = w
	p.workspace.view.Height = max(1, h)
	p.workspace.view.SetContent(ansi.Hardwrap(safePanel(p.workspace.body), w, true))
	lines = append(lines, p.workspace.view.View())
	if p.workspace.loading {
		lines = append(lines, "Carregando… · Esc cancela")
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
	case "settings", "providers", "models", "integrations", "sessions", "process", "latest", "queue", "context", "compact", "skills", "skill", "changes", "checks", "undo", "redo", "actions", "export", "permissions", "retry", "error":
	default:
		return nil, false
	}
	if len(attachments) > 0 {
		err := fmt.Errorf("este comando não aceita anexos; o rascunho foi preservado")
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
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		p.workspace.view, _ = p.workspace.view.Update(msg)
		return nil
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.Y >= 2 {
		h := max(1, min(10, p.height-10))
		start := max(0, min(p.workspace.selected-h+1, len(p.workspace.actions)-h))
		index := start + msg.Y - 2
		if msg.Y < 2+h && index >= 0 && index < len(p.workspace.actions) {
			p.workspace.selected = index
			return p.workspaceChoose()
		}
	}
	return nil
}

func (p *chatPage) retryRequest(draft string) tea.Cmd {
	if p.lastRequest.Text == "" {
		err := fmt.Errorf("nenhuma pergunta anterior disponível")
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
