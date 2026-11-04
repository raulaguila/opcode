package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/fileutil"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/transcript"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/workflow"
)

type command struct {
	title, desc, key, group string
	run                     func(*Model) tea.Cmd
}

func (m *Model) commands() []command {
	main := textFor("Principais", "Main")
	work := textFor("Trabalho", "Work")
	settings := textFor("Configurações", "Settings")
	conversation := textFor("Conversa", "Conversation")
	mode := func(id string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { m.setMode(id); return nil }
	}
	return []command{
		{textFor("Nova conversa", "New conversation"), textFor("começar do zero", "start fresh"), "Ctrl+N", main, (*Model).newConversation},
		{textFor("Escolher modelo", "Choose model"), textFor("modelo e provedor", "model and provider"), "Ctrl+O", main, (*Model).openModels},
		{textFor("Conversas", "Conversations"), textFor("abrir, renomear, arquivar", "open, rename, archive"), "Ctrl+S", main, (*Model).openSessions},
		{textFor("Configurações", "Settings"), textFor("conexões e preferências", "connections and preferences"), "/settings", main, (*Model).openSettings},
		{textFor("Perguntar", "Ask"), textFor("lê e explica, sem alterar", "reads and explains, no changes"), "Shift+Tab", work, mode("ask")},
		{textFor("Planejar", "Plan"), textFor("prepara um plano, sem editar", "prepares a plan, no edits"), "Shift+Tab", work, mode("plan")},
		{textFor("Executar", "Execute"), textFor("altera arquivos com aprovação", "changes files with approval"), "Shift+Tab", work, mode("execute")},
		{textFor("Plano", "Plan"), textFor("mostrar ou recolher o plano", "show or fold the plan"), "/plan", work, (*Model).togglePlan},
		{textFor("Retomar tarefa", "Resume task"), textFor("abre a última conversa e o plano salvo", "opens the last conversation and its saved plan"), "/resume", work, (*Model).resumeLast},
		{textFor("Responder pergunta", "Answer question"), textFor("reabre a pergunta pendente do agente", "reopens the agent's pending question"), "Enter", work, func(m *Model) tea.Cmd { m.laterQuestion = ""; m.layout(); return nil }},
		{textFor("Cancelar execução", "Cancel run"), textFor("para a tarefa em andamento", "stops the running task"), "Esc", work, func(m *Model) tea.Cmd {
			if m.busy && m.app.CoderAgent != nil {
				m.app.CoderAgent.Cancel(m.session.ID)
			}
			return nil
		}},
		{textFor("Alterações", "Changes"), textFor("arquivos alterados por interação; desfazer ou refazer", "files changed per turn; undo or redo"), "/changes", work, (*Model).openChanges},
		{textFor("Verificações", "Checks"), textFor("resultado das verificações da tarefa", "the task's check results"), "/checks", work, (*Model).openChecks},
		{textFor("Comandos", "Commands"), textFor("comando completo, saída e cópia", "full command, output and copy"), "Ctrl+P", work, (*Model).openCommands},
		{textFor("Fila", "Queue"), textFor("mensagens enviadas durante a execução", "messages sent while the agent works"), "/queue", work, (*Model).openQueue},
		{textFor("Skills", "Skills"), textFor("instruções prontas da configuração global", "ready-made instructions from the global configuration"), "/skills", work, (*Model).openSkills},
		{textFor("Desfazer interação", "Undo turn"), textFor("reverte as edições da última resposta", "reverts the last reply's edits"), "/undo", work, func(m *Model) tea.Cmd { return m.undoLast(false) }},
		{textFor("Refazer interação", "Redo turn"), textFor("reaplica a interação desfeita", "reapplies the undone turn"), "/redo", work, func(m *Model) tea.Cmd { return m.undoLast(true) }},
		{textFor("Contexto", "Context"), textFor("quanto da janela do modelo a conversa usa", "how much of the model window the conversation uses"), "/context", conversation, func(m *Model) tea.Cmd { return m.showContext(false) }},
		{textFor("Compactar conversa", "Compact conversation"), textFor("resume saídas antigas para liberar contexto", "folds old output to free context"), "/compact", conversation, func(m *Model) tea.Cmd { return m.showContext(true) }},
		{textFor("Exportar conversa", "Export conversation"), textFor("salva em Markdown", "saves as Markdown"), "/export", conversation, (*Model).exportCurrent},
		{textFor("Conexões", "Connections"), textFor("provedores e chaves", "providers and keys"), "/connections", settings, func(m *Model) tea.Cmd { return m.openSettingsAt(sectionConnections) }},
		{textFor("Preferências", "Preferences"), textFor("idioma, notificações, modo inicial", "language, notifications, initial mode"), "/preferences", settings, func(m *Model) tea.Cmd { return m.openSettingsAt(sectionInterface) }},
		{textFor("Atalhos", "Shortcuts"), textFor("teclas dos comandos", "command keys"), "/keys", settings, func(m *Model) tea.Cmd { return m.openSettingsAt(sectionShortcuts) }},
		{textFor("Aprovação", "Approval"), textFor("o que o agente faz sem perguntar", "what the agent does without asking"), "/approval", settings, (*Model).openApproval},
		{textFor("Sair", "Quit"), textFor("fechar o opcode", "close opcode"), "Ctrl+C", conversation, func(m *Model) tea.Cmd { m.dialog = confirmQuit{}; return nil }},
	}
}

func (m *Model) openPalette() tea.Cmd {
	cmds := m.commands()
	// The commands used last lead, under Recent; they are kept for this run.
	var items []Item
	for _, title := range m.recent {
		for i, c := range cmds {
			if c.title == title {
				items = append(items, Item{ID: fmt.Sprint(i), Title: c.title, Desc: c.desc, Right: c.key, Group: textFor("Recentes", "Recent"), Value: c.run})
			}
		}
	}
	for i, c := range cmds {
		if !slices.Contains(m.recent, c.title) {
			items = append(items, Item{ID: fmt.Sprint(i), Title: c.title, Desc: c.desc, Right: c.key, Group: c.group, Value: c.run})
		}
	}
	m.dialog = &listDialog{
		title:        textFor("Comandos", "Commands"),
		legend:       textFor("↑↓ escolher · Enter abrir · Esc fechar", "↑↓ choose · Enter open · Esc close"),
		searchPrompt: "/",
		width:        74,
		placeholder:  textFor("Buscar comando", "Search commands"),
		list:         List{Items: items, Grouped: true, Cursor: "◆", Chips: true},
		meta: func(d *listDialog) string {
			if d.list.Query != "" {
				return results(len(d.list.Visible()))
			}
			return fmt.Sprint(len(d.list.Items))
		},
		choose: func(m *Model, item Item) tea.Cmd {
			m.dialog = nil
			m.remember(item.Title)
			return item.Value.(func(*Model) tea.Cmd)(m)
		},
	}
	return nil
}

// setMode changes the mode; when the agent refuses it, the screen keeps the
// mode the agent is in and says why.
func (m *Model) setMode(mode string) {
	if m.session.ID != "" && m.app.CoderAgent != nil {
		if err := m.app.CoderAgent.SetMode(context.Background(), m.session.ID, workflow.Mode(mode)); err != nil {
			m.notice = err.Error()
			return
		}
	}
	m.mode = mode
	m.layout()
}

// approvalName says who decides, so it cannot be read as a work mode.
func approvalName(policy permission.ApprovalPolicy) string {
	switch policy {
	case permission.ApprovalAuto:
		return textFor("Aprovar por mim", "Approve for me")
	case permission.ApprovalFull:
		return textFor("Acesso total", "Full access")
	}
	return textFor("Pedir aprovação", "Ask for approval")
}

// approvalLevels go from least to most autonomy.
var approvalLevels = []permission.ApprovalPolicy{permission.ApprovalSandboxed, permission.ApprovalAuto, permission.ApprovalFull}

// approvalNote says what a level frees and what still asks.
func approvalNote(policy permission.ApprovalPolicy) (free, asks string) {
	switch policy {
	case permission.ApprovalAuto:
		free = textFor("Editar o projeto, rodar comandos no sandbox e baixar as dependências declaradas.", "Editing the project, running commands in the sandbox and fetching declared dependencies.")
		asks = textFor("Só ações potencialmente inseguras: outra internet e arquivos fora do projeto.", "Only potentially unsafe actions: other internet use and files outside the project.")
	case permission.ApprovalFull:
		return textFor("Tudo, sem sandbox: internet e qualquer arquivo do computador.", "Everything, without the sandbox: internet and any file on your computer."),
			textFor("Nada. Vale só nesta conversa.", "Nothing. Applies to this conversation only.")
	default:
		free = textFor("Editar o projeto e rodar comandos no sandbox.", "Editing the project and running commands in the sandbox.")
		asks = textFor("Internet e arquivos fora do projeto.", "Internet and files outside the project.")
	}
	if !shell.SandboxActive() {
		asks += " " + textFor("Sandbox desligado: todo comando também pede.", "Sandbox off: every command asks too.")
	}
	return free, asks
}

// approvalDetail is the side column of the approval menu.
func approvalDetail(_ *Model, item Item, width int) []string {
	policy := item.Value.(permission.ApprovalPolicy)
	free, asks := approvalNote(policy)
	body := P.Muted
	if policy == permission.ApprovalFull {
		body = P.Danger
	}
	var rows []string
	section := func(title, text string) {
		rows = append(rows, on(P.Surface, P.Strong).Bold(true).Render(title))
		for _, line := range strings.Split(ansi.Wrap(text, width, ""), "\n") {
			rows = append(rows, on(P.Surface, body).Render(line))
		}
	}
	section(textFor("Sem perguntar", "Without asking"), free)
	rows = append(rows, "")
	section(textFor("Ainda pede", "Still asks"), asks)
	return rows
}

// openApproval is the one place that chooses what runs without asking.
func (m *Model) openApproval() tea.Cmd {
	selected := 0
	items := make([]Item, len(approvalLevels))
	for i, policy := range approvalLevels {
		items[i] = Item{ID: string(policy), Title: approvalName(policy), Value: policy}
		if policy == m.policy {
			selected = i
			items[i].Mark = "✓"
		}
		if m.width < 80 { // too narrow for the side column
			free, _ := approvalNote(policy)
			items[i].Desc = free
		}
	}
	armed := false // full access takes a second Enter
	dialog := &listDialog{
		title:    textFor("Aprovação", "Approval"),
		legend:   textFor("↑↓ escolher · Enter aplicar · Esc fechar", "↑↓ choose · Enter apply · Esc close"),
		noSearch: true,
		width:    84,
		split:    36,
		list:     List{Items: items, Cursor: "◆", Selected: selected},
		meta:     func(*listDialog) string { return "" },
		side:     approvalDetail,
	}
	dialog.choose = func(m *Model, item Item) tea.Cmd {
		if m.busy {
			m.dialog, m.notice = nil, textFor("Aguarde ou cancele a execução antes de alterar as aprovações.", "Wait or cancel the current run before changing approvals.")
			return nil
		}
		policy := item.Value.(permission.ApprovalPolicy)
		if policy == permission.ApprovalFull && m.policy != policy && !armed {
			armed = true
			dialog.legend = textFor("Enter de novo libera acesso total · Esc cancelar", "Enter again allows full access · Esc cancel")
			return nil
		}
		m.dialog = nil
		m.setApproval(policy)
		return nil
	}
	m.dialog = dialog
	return nil
}

// leaveFullAccess keeps full access from outliving its conversation.
func (m *Model) leaveFullAccess() {
	if m.policy == permission.ApprovalFull {
		m.setApproval(permission.ApprovalSandboxed)
	}
}

func (m *Model) setApproval(policy permission.ApprovalPolicy) {
	m.policy = policy
	if policies, ok := m.app.Permissions.(permission.ApprovalPolicies); ok && m.session.ID != "" {
		if err := policies.SetApprovalPolicy(m.session.ID, policy, config.WorkingDirectory()); err != nil {
			m.notice = err.Error()
		}
	}
}

func (m *Model) newConversation() tea.Cmd {
	m.leaveFullAccess()
	m.foldPlan, m.task = false, workflow.Task{}
	m.session, m.messages, m.notice, m.busy = session.Session{}, nil, "", false
	m.version++
	m.refresh()
	return nil
}

func results(n int) string {
	if n == 1 {
		return textFor("1 resultado", "1 result")
	}
	return fmt.Sprintf(textFor("%d resultados", "%d results"), n)
}

// resumeLast opens the most recent other conversation, its plan unfolded.
func (m *Model) resumeLast() tea.Cmd {
	if m.app == nil || m.app.Sessions == nil {
		return nil
	}
	list, err := m.app.Sessions.List(context.Background())
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	slices.SortFunc(list, func(a, b session.Session) int { return int(b.UpdatedAt - a.UpdatedAt) })
	for _, s := range list {
		if s.ParentSessionID == "" && s.ID != m.session.ID {
			m.openSession(s)
			m.poll()
			m.foldPlan = false
			m.layout()
			return nil
		}
	}
	m.info = textFor("Nenhuma outra conversa para retomar", "No other conversation to resume")
	return nil
}

// exportCurrent saves this conversation as Markdown.
func (m *Model) exportCurrent() tea.Cmd {
	if m.session.ID == "" || m.app == nil {
		m.info = textFor("Nada para exportar ainda", "Nothing to export yet")
		return nil
	}
	messages, err := m.app.Messages.List(context.Background(), m.session.ID)
	if err == nil {
		cfg := config.Get()
		var path string
		if path, err = transcript.Save(cfg.Data.Directory, cfg.WorkingDir, m.session, messages, tools.RedactCredentials); err == nil {
			m.info = textFor("Conversa exportada: ", "Conversation exported: ") + fileutil.HomeRelative(path)
		}
	}
	if err != nil {
		m.notice = err.Error()
	}
	return nil
}

// remember puts a command first among the recent ones, keeping three.
func (m *Model) remember(title string) {
	recent := []string{title}
	for _, other := range m.recent {
		if other != title && len(recent) < 3 {
			recent = append(recent, other)
		}
	}
	m.recent = recent
}
