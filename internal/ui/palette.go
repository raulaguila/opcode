package ui

import (
	"context"
	"fmt"
	"slices"

	"github.com/raulaguila/opcode/internal/fileutil"
	"github.com/raulaguila/opcode/internal/llm/tools"
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
	approval := func(policy permission.ApprovalPolicy) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { m.setApproval(policy); return nil }
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
		{textFor("Desfazer interação", "Undo turn"), textFor("reverte as edições da última resposta", "reverts the last reply's edits"), "/undo", work, func(m *Model) tea.Cmd { return m.undoLast(false) }},
		{textFor("Refazer interação", "Redo turn"), textFor("reaplica a interação desfeita", "reapplies the undone turn"), "/redo", work, func(m *Model) tea.Cmd { return m.undoLast(true) }},
		{textFor("Exportar conversa", "Export conversation"), textFor("salva em Markdown", "saves as Markdown"), "/export", conversation, (*Model).exportCurrent},
		{textFor("Conexões", "Connections"), textFor("provedores e chaves", "providers and keys"), "/connections", settings, func(m *Model) tea.Cmd { return m.openSettingsAt(sectionConnections) }},
		{textFor("Preferências", "Preferences"), textFor("idioma, notificações, modo inicial", "language, notifications, initial mode"), "/preferences", settings, func(m *Model) tea.Cmd { return m.openSettingsAt(sectionInterface) }},
		{textFor("Atalhos", "Shortcuts"), textFor("teclas dos comandos", "command keys"), "/keys", settings, func(m *Model) tea.Cmd { return m.openSettingsAt(sectionShortcuts) }},
		{textFor("Aprovação: pedir", "Approval: ask"), textFor("pede antes de cada alteração", "asks before every change"), "/approval", settings, approval(permission.ApprovalPrompt)},
		{textFor("Aprovação: editar no projeto", "Approval: project edits"), textFor("edita arquivos do projeto sem pedir", "edits project files without asking"), "/approval", settings, approval(permission.ApprovalProjectEdits)},
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

func (m *Model) setApproval(policy permission.ApprovalPolicy) {
	m.policy = policy
	if policies, ok := m.app.Permissions.(permission.ApprovalPolicies); ok && m.session.ID != "" {
		if err := policies.SetApprovalPolicy(m.session.ID, policy, config.WorkingDirectory()); err != nil {
			m.notice = err.Error()
		}
	}
}

func (m *Model) newConversation() tea.Cmd {
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

// openPaletteWith opens the palette already filtered, as a footer control does.
func (m *Model) openPaletteWith(query string) tea.Cmd {
	cmd := m.openPalette()
	if d, ok := m.dialog.(*listDialog); ok {
		d.list.SetQuery(query)
	}
	return cmd
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
			m.showPlan = len(m.task.Steps) > 0
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
