package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/llm/agent"
)

// openChecks lists the task's check receipts: which passed, which failed.
func (m *Model) openChecks() tea.Cmd {
	var items []Item
	for _, receipt := range m.task.Validation {
		item := Item{ID: receipt.Name, Title: receipt.Name, Desc: time.Unix(receipt.At, 0).Format("02/01 15:04"), Right: textFor("passou", "passed"), Mark: "✓"}
		if !receipt.Passed {
			item.Right, item.Mark, item.MarkColor = textFor("falhou", "failed"), "✗", P.Danger
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		m.info = textFor("Nenhuma verificação registrada nesta tarefa", "No checks recorded for this task")
		return nil
	}
	m.dialog = &listDialog{
		title:    textFor("Verificações", "Checks"),
		legend:   textFor("↑↓ escolher · Esc fechar", "↑↓ choose · Esc close"),
		noSearch: true,
		width:    74,
		list:     List{Items: items, Cursor: "◆"},
		meta:     func(d *listDialog) string { return fmt.Sprint(len(d.list.Items)) },
		choose:   func(m *Model, _ Item) tea.Cmd { m.dialog = nil; return nil },
	}
	return nil
}

// openQueue lists messages sent while the agent works; Enter takes one back
// into an empty composer, Delete drops it.
func (m *Model) openQueue() tea.Cmd {
	control, ok := m.app.CoderAgent.(agent.QueueControl)
	var items []Item
	if ok {
		for _, queued := range control.QueuedInstructions(m.session.ID) {
			items = append(items, Item{ID: queued.ID, Title: strings.Join(strings.Fields(queued.Content), " ")})
		}
	}
	if len(items) == 0 {
		m.info = textFor("Nenhuma mensagem na fila", "No queued messages")
		return nil
	}
	take := func(m *Model, item Item) error {
		_, err := control.TakeInstruction(m.session.ID, item.ID)
		return err
	}
	m.dialog = &listDialog{
		title:    textFor("Fila", "Queue"),
		legend:   textFor("↑↓ escolher · Enter editar · Delete remover · Esc fechar", "↑↓ choose · Enter edit · Delete remove · Esc close"),
		noSearch: true,
		width:    90,
		list:     List{Items: items, Cursor: "◆"},
		meta:     func(d *listDialog) string { return fmt.Sprint(len(d.list.Items)) },
		choose: func(m *Model, item Item) tea.Cmd {
			if strings.TrimSpace(m.composer.Value()) != "" {
				m.notice = textFor("Envie ou limpe o rascunho antes de editar a mensagem da fila", "Send or clear your draft before editing a queued message")
				return nil
			}
			m.dialog = nil
			if err := take(m, item); err != nil {
				m.notice = err.Error()
				return nil
			}
			m.composer.SetValue(item.Title)
			return nil
		},
		keys: func(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
			if msg.String() != "delete" {
				return nil, false
			}
			if item, ok := m.dialog.(*listDialog).list.Current(); ok {
				if err := take(m, item); err != nil {
					m.notice = err.Error()
				}
			}
			m.dialog = nil
			return m.openQueue(), true
		},
	}
	return nil
}

// openSkills lists the configured skills; Enter starts "/skill NAME " in the
// composer, where the request after the name goes to the skill.
func (m *Model) openSkills() tea.Cmd {
	control, ok := m.app.CoderAgent.(agent.ExtensionControl)
	if !ok {
		m.info = textFor("Skills indisponíveis neste agente", "Skills unavailable for this agent")
		return nil
	}
	skills, err := control.Skills(context.Background())
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	var items []Item
	for _, skill := range skills {
		items = append(items, Item{ID: skill.Name, Title: skill.Name, Desc: skill.Description})
	}
	if len(items) == 0 {
		m.info = textFor("Nenhuma skill configurada: veja skills.directories na configuração global", "No skills configured: see skills.directories in the global configuration")
		return nil
	}
	m.dialog = &listDialog{
		title:        textFor("Skills", "Skills"),
		legend:       textFor("↑↓ escolher · Enter usar · Esc fechar", "↑↓ choose · Enter use · Esc close"),
		searchPrompt: "/",
		placeholder:  textFor("Buscar skill", "Search skills"),
		width:        90,
		list:         List{Items: items, Cursor: "◆"},
		meta:         func(d *listDialog) string { return fmt.Sprint(len(d.list.Items)) },
		choose: func(m *Model, item Item) tea.Cmd {
			m.dialog = nil
			m.composer.SetValue("/skill " + item.Title + " ")
			return nil
		},
	}
	return nil
}

// invokeCommand runs "/auditoria" or "/skill NAME request" instead of sending a
// plain message; ok is false when the text is neither.
func (m *Model) invokeCommand(ctx context.Context, text string) (bool, error) {
	if control, ok := m.app.CoderAgent.(agent.AuditControl); ok && agent.IsAuditCommand(text) {
		_, err := control.Audit(ctx, m.session.ID)
		return true, err
	}
	rest, found := strings.CutPrefix(text, "/skill ")
	control, ok := m.app.CoderAgent.(agent.ExtensionControl)
	if !found || !ok {
		return false, nil
	}
	name, args, _ := strings.Cut(strings.TrimSpace(rest), " ")
	_, _, err := control.InvokeSkill(ctx, m.session.ID, name, strings.TrimSpace(args))
	return true, err
}
