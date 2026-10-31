package page

import (
	"github.com/raulaguila/opcode/internal/tui/util"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
)

// The plan and recovery actions stay beside the composer, where the next
// instruction is entered. They never replace the conversation being read.
func (p *chatPage) composerPanel() string {
	var panels []string
	if view := p.workflowView(); view != "" {
		panels = append(panels, view)
	}
	if p.recoveryHeight() > 0 {
		state := recoveryTitle(p.recovery.err)
		if p.recovery.resolved {
			state = config.Text("Execução retomada", "Run resumed")
		}
		first := state + " · " + config.Text("Rascunho e progresso preservados", "Draft and progress preserved")
		panels = append(panels, ansi.Truncate(first, max(1, p.width), "…")+"\n"+ansi.Truncate(recoveryActions(p.recovery.err), max(1, p.width), "…"))
	}
	return strings.Join(panels, "\n")
}
func (p *chatPage) recoveryHeight() int {
	if p.recovery.err == nil || p.recovery.resolved || p.width < 20 || p.height < 12 {
		return 0
	}
	return 2
}
func (p *chatPage) composerPanelHeight() int { return p.workflowHeight() + p.recoveryHeight() }
func (p *chatPage) insertComposerPanel(view string) string {
	panel := p.composerPanel()
	if panel == "" {
		return view
	}
	_, h := p.layout.GetSize()
	_, editorHeight := p.editor.GetSize()
	lines := strings.Split(view, "\n")
	offset := max(0, min(len(lines), h-editorHeight))
	lines = append(lines[:offset], append(strings.Split(panel, "\n"), lines[offset:]...)...)
	return strings.Join(lines, "\n")
}
func (p *chatPage) recoveryMouse(msg tea.MouseMsg) tea.Cmd {
	if !util.MousePressed(msg) || msg.Mouse().Button != tea.MouseLeft {
		return nil
	}
	_, height := p.layout.GetSize()
	_, editorHeight := p.editor.GetSize()
	if msg.Mouse().Y != height-editorHeight+p.workflowHeight()+1 {
		return nil
	}
	actions := recoveryActions(p.recovery.err)
	commands := []string{"retry", "error", "settings", "context", "models", "changes", "local-status"}
	chosen := "error"
	best := -1
	for _, command := range commands {
		idx := strings.Index(actions, "/"+command)
		pos := -1
		if idx >= 0 {
			pos = ansi.StringWidth(actions[:idx])
		}
		if pos >= 0 && pos <= msg.Mouse().X && pos > best {
			best = pos
			chosen = command
		}
	}
	return p.workspaceCommand(chosen, "")
}
func (p *chatPage) queueSummary() string {
	if p.app == nil || p.app.CoderAgent == nil {
		return ""
	}
	if queue, ok := p.app.CoderAgent.(agent.QueueControl); ok {
		if n := len(queue.QueuedInstructions(p.session.ID)); n > 0 {
			return p.pendingLabel(n)
		}
	}
	return ""
}
func (p *chatPage) PersistDraft() error {
	if drafts, ok := p.composer.(interface{ PersistDraft() error }); ok {
		return drafts.PersistDraft()
	}
	return nil
}

func (p *chatPage) PrepareSessionDraft(id string) error {
	if id == p.session.ID {
		return nil
	}
	if drafts, ok := p.composer.(interface{ PrepareSessionDraft(string) error }); ok {
		return drafts.PrepareSessionDraft(id)
	}
	return nil
}

// Recovery suggestions never retry automatically or discard a draft.
func recoveryTitle(err error) string {
	if err == nil {
		return ""
	}
	switch classifyRecovery(err) {
	case recoveryCitation:
		return config.Text("Citações não validadas", "Citations not validated")
	case recoveryAuthentication:
		return config.Text("Autenticação necessária", "Authentication required")
	case recoveryMemory:
		return config.Text("Memória insuficiente no servidor", "Server memory exhausted")
	case recoveryCancelled:
		return config.Text("Execução cancelada", "Run cancelled")
	case recoveryUnavailable:
		return config.Text("Provedor indisponível", "Provider unavailable")
	case recoveryCapacity:
		return config.Text("Limite do modelo atingido", "Model limit reached")
	case recoveryConflict:
		return config.Text("Conflito de arquivo", "File conflict")
	case recoveryBudget:
		return config.Text("Limite da execução atingido", "Run limit reached")
	default:
		return config.Text("Execução interrompida", "Run interrupted")
	}
}
func recoveryActions(err error) string {
	title := recoveryTitle(err)
	switch title {
	case config.Text("Citações não validadas", "Citations not validated"):
		return config.Text(" /error Ver detalhes · /retry Solicitar correção", " /error View details · /retry Request correction")
	case config.Text("Limite da execução atingido", "Run limit reached"):
		return config.Text(" /task Rever tarefa · /error Detalhes", " /task Review task · /error Details")
	case config.Text("Autenticação necessária", "Authentication required"):
		return config.Text(" /settings Configurar acesso · /error Detalhes", " /settings Configure access · /error Details")
	case config.Text("Limite do modelo atingido", "Model limit reached"):
		return config.Text(" /context Rever contexto · /models Trocar modelo · /error", " /context Review context · /models Change model · /error")
	case config.Text("Memória insuficiente no servidor", "Server memory exhausted"):
		return config.Text(" /models Escolher modelo menor · /local-status Alocação · /error Detalhes", " /models Choose smaller model · /local-status Allocation · /error Details")
	case config.Text("Provedor indisponível", "Provider unavailable"):
		return config.Text(" /retry Tentar novamente · /models Trocar modelo · /error Detalhes", " /retry Try again · /models Change model · /error Details")
	case config.Text("Conflito de arquivo", "File conflict"):
		return config.Text(" /changes Revisar arquivos · /error Detalhes", " /changes Review files · /error Details")
	default:
		return config.Text(" /retry Retomar · /error Detalhes · /settings Configuração", " /retry Resume · /error Details · /settings Configuration")
	}
}

// RecoverySummary is safe application copy for the footer; raw details stay in /error.
func RecoverySummary(err error) string {
	return recoveryTitle(err) + config.Text(" · /error detalhes", " · /error details")
}
