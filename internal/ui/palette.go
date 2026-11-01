package ui

import (
	"context"
	"fmt"

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
		{textFor("Aprovação: pedir", "Approval: ask"), textFor("pede antes de cada alteração", "asks before every change"), "/approval", settings, approval(permission.ApprovalPrompt)},
		{textFor("Aprovação: editar no projeto", "Approval: project edits"), textFor("edita arquivos do projeto sem pedir", "edits project files without asking"), "/approval", settings, approval(permission.ApprovalProjectEdits)},
		{textFor("Sair", "Quit"), textFor("fechar o opcode", "close opcode"), "Ctrl+C", conversation, func(*Model) tea.Cmd { return tea.Quit }},
	}
}

func (m *Model) openPalette() tea.Cmd {
	cmds := m.commands()
	items := make([]Item, len(cmds))
	for i, c := range cmds {
		items[i] = Item{ID: fmt.Sprint(i), Title: c.title, Desc: c.desc, Right: c.key, Group: c.group, Value: c.run}
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
			return item.Value.(func(*Model) tea.Cmd)(m)
		},
	}
	return nil
}

func (m *Model) setMode(mode string) {
	m.mode = mode
	if m.session.ID != "" && m.app.CoderAgent != nil {
		_ = m.app.CoderAgent.SetMode(context.Background(), m.session.ID, workflow.Mode(mode))
	}
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
