package page

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/raulaguila/opcode/internal/workflow"
)

type GitWorkspaceMsg struct {
	Session  string
	Sequence uint64
	Trees    []tools.Worktree
	Status   string
	Err      error
}

func (p *chatPage) openGitWorkspace() tea.Cmd {
	p.showWorkspace("worktrees", config.Text("Projeto · Git e worktrees", "Project · Git and worktrees"), config.WorkingDirectory())
	p.workspace.loading = true
	seq, id := p.workspace.seq, p.session.ID
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	p.workspace.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		trees, err := tools.ListWorktrees(ctx)
		if err != nil {
			return GitWorkspaceMsg{Session: id, Sequence: seq, Err: err}
		}
		response, err := tools.NewGitTools()[0].Run(ctx, tools.ToolCall{Input: `{"limit":100}`})
		return GitWorkspaceMsg{Session: id, Sequence: seq, Trees: trees, Status: response.Content, Err: err}
	}
}
func (p *chatPage) gitWorkspaceResult(msg GitWorkspaceMsg) tea.Cmd {
	if msg.Session != p.session.ID || msg.Sequence != p.workspace.seq || p.workspace.kind != "worktrees" {
		return nil
	}
	p.workspace.loading = false
	p.workspace.cancel = nil
	if msg.Err != nil {
		p.workspace.note = panelText(msg.Err.Error())
		return nil
	}
	branch := config.Text("Não identificado", "Unknown")
	for _, tree := range msg.Trees {
		name := strings.TrimPrefix(tree.Branch, "refs/heads/")
		if tree.Detached {
			name = "detached HEAD"
		}
		if filepath.Clean(tree.Path) == filepath.Clean(config.WorkingDirectory()) {
			branch = name
			continue
		}
		p.workspace.actions = append(p.workspace.actions, workspaceAction{kind: "worktree-open", path: tree.Path, label: config.Text("Abrir ", "Open ") + panelText(name) + " · " + panelText(tree.Path)})
	}
	p.workspace.actions = append(p.workspace.actions, workspaceAction{kind: "git-diff", label: config.Text("Revisar diferenças Git", "Review Git diff")}, workspaceAction{kind: "git-diff-staged", label: config.Text("Revisar diferenças preparadas", "Review staged diff")}, workspaceAction{kind: "git-checks", label: config.Text("Verificações e alterações da tarefa", "Task checks and changes")}, workspaceAction{kind: "worktree-create", label: config.Text("Criar worktree isolada (aprovação)", "Create isolated worktree (approval)")})
	var status struct {
		Entries   []struct{ Status, Path string }
		Truncated bool
	}
	_ = json.Unmarshal([]byte(msg.Status), &status)
	lines := []string{config.Text("Projeto: ", "Project: ") + config.WorkingDirectory(), config.Text("Branch: ", "Branch: ") + panelText(branch)}
	for _, entry := range status.Entries {
		lines = append(lines, panelText(entry.Status+" "+entry.Path))
	}
	if len(status.Entries) == 0 {
		lines = append(lines, config.Text("Nenhuma alteração Git detectada.", "No Git changes detected."))
	}
	if status.Truncated {
		lines = append(lines, config.Text("Listagem limitada a 100 arquivos.", "Listing limited to 100 files."))
	}
	lines = append(lines, config.Text("Worktrees abrem em uma sessão isolada. Ao sair, você retorna aqui; permissões e processos não são transferidos. Nenhum commit ou merge automático.", "Worktrees open in an isolated session. Exiting returns here; permissions and processes are not transferred. No automatic commits or merges."))
	p.workspace.body = strings.Join(lines, "\n")
	return nil
}
func (p *chatPage) gitChoose(item workspaceAction) tea.Cmd {
	if item.kind == "worktree-create" || item.kind == "worktree-create-confirm" {
		if p.mode() != workflow.Execute {
			return util.ReportWarn(config.Text("Criar worktree exige modo Executar. Use /mode execute e confira /contract.", "Creating a worktree requires Execute mode. Use /mode execute and inspect /contract."))
		}
		if p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy() {
			return util.ReportWarn(config.Text("Aguarde o agente antes de criar uma worktree.", "Wait for the agent before creating a worktree."))
		}
	}

	switch item.kind {
	case "git-checks":
		return p.openChanges()
	case "git-diff", "git-diff-staged":
		staged := item.kind == "git-diff-staged"
		p.showWorkspace("turn", config.Text("Diferenças Git", "Git diff"), "")
		return p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
			raw, _ := json.Marshal(map[string]any{"staged": staged, "limit": 100})
			reply, err := tools.NewGitTools()[1].Run(ctx, tools.ToolCall{Input: string(raw)})
			if err != nil {
				return "", false, err
			}
			var result struct {
				Diff      string
				Truncated bool
			}
			if json.Unmarshal([]byte(reply.Content), &result) != nil {
				return "", false, fmt.Errorf("invalid diff result")
			}
			text := result.Diff
			if result.Truncated {
				text += "\n[truncated]"
			}
			return text, false, nil
		})
	case "worktree-create":
		// A generated name avoids path interpolation. The exact protected Git argv
		// still passes the existing permission dialog and sandbox.
		name := "task-" + time.Now().Format("20060102-150405")
		p.showWorkspace("worktree-create-confirm", config.Text("Criar worktree: ", "Create worktree: ")+name, config.Text("Cria a partir de HEAD com arquivos já commitados; mudanças locais permanecem aqui. Enter solicita aprovação; Esc cancela.", "Creates from committed HEAD; local changes stay here. Enter requests approval; Esc cancels."))
		p.workspace.actions = []workspaceAction{{kind: "worktree-create-confirm", text: name, label: config.Text("Criar e revisar", "Create and review")}}
		return nil
	case "worktree-create-confirm":
		var selected tea.Cmd
		if p.session.ID == "" {
			s, err := p.app.Sessions.Create(context.Background(), config.Text("Worktree", "Worktree"))
			if err != nil {
				return util.ReportError(err)
			}
			p.session = s
			selected = util.CmdHandler(chat.SessionSelectedMsg(s))
		}
		p.showWorkspace("worktree-create-result", config.Text("Criar worktree", "Create worktree"), "")
		service, id := p.app.Permissions, p.session.ID
		return tea.Batch(selected, p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
			ctx = context.WithValue(ctx, tools.SessionIDContextKey, id)
			raw, _ := json.Marshal(map[string]string{"action": "create", "name": item.text})
			response, err := tools.NewWorktreeTool(service).Run(ctx, tools.ToolCall{Input: string(raw)})
			if err != nil {
				return "", false, err
			}
			if response.IsError {
				return "", false, fmt.Errorf("%s", response.Content)
			}
			return response.Content + "\n/worktrees", false, nil
		}))
	case "worktree-open":
		p.showWorkspace("worktree-open-confirm", config.Text("Abrir projeto isolado", "Open isolated project"), panelText(item.path)+"\n\n"+config.Text("Enter abre outra instância do Opcode neste terminal. Ao sair dela, você volta à conversa atual. Nenhum processo ou autorização será transferido.", "Enter opens another Opcode instance in this terminal. Exiting returns to this conversation. Processes and approvals are not transferred."))
		p.workspace.actions = []workspaceAction{{kind: "worktree-open-confirm", path: item.path, label: config.Text("Abrir", "Open")}}
		return nil
	case "worktree-open-confirm":
		if err := p.canOpenWorktree(item.path); err != nil {
			return util.ReportWarn(err.Error())
		}
		exe, err := os.Executable()
		if err != nil {
			return util.ReportError(err)
		}
		command := exec.Command(exe, "--cwd", item.path)
		p.closeWorkspace()
		return tea.ExecProcess(command, func(err error) tea.Msg {
			if err != nil {
				return util.InfoMsg{Type: util.InfoTypeError, Msg: config.Text("A instância da worktree terminou com erro.", "The worktree instance exited with an error.")}
			}
			return util.InfoMsg{Msg: config.Text("Conversa original retomada.", "Original conversation resumed.")}
		})
	}
	return nil
}
func (p *chatPage) canOpenWorktree(path string) error {
	if p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy() {
		return fmt.Errorf("%s", config.Text("Aguarde ou cancele o agente antes de trocar de projeto.", "Wait or cancel the agent before switching projects."))
	}
	if p.app.Processes != nil {
		for _, process := range p.app.Processes.List(p.session.ID) {
			if process.Running {
				return fmt.Errorf("%s", config.Text("Encerre os processos desta conversa antes de abrir outro projeto.", "Stop this conversation's processes before opening another project."))
			}
		}
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != filepath.Clean(path) {
		return fmt.Errorf("worktree path is missing or contains symbolic links")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	trees, err := tools.ListWorktrees(ctx)
	if err != nil {
		return err
	}
	for _, tree := range trees {
		if tree.Path == path {
			return nil
		}
	}
	return fmt.Errorf("worktree is no longer registered")
}
