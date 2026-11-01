package page

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

// headerRows is the space the top bar takes: the bar, then a rule under it
// when the terminal is tall enough.
func (p *chatPage) headerRows() int {
	switch {
	case p.height >= 20:
		return 2
	case p.height >= 12:
		return 1
	}
	return 0
}

func (p *chatPage) headerRule() string {
	t := theme.CurrentTheme()
	return styles.BaseStyle().Foreground(t.BorderNormal()).Render(strings.Repeat("─", max(1, p.width)))
}

// headerView is the top bar: product, project path and Git branch on the
// left, the conversation title on the right.
func (p *chatPage) headerView() string {
	t := theme.CurrentTheme()
	bar := styles.BaseStyle().Background(t.Background())
	muted := styles.HintOn(t.Background())
	left := bar.Bold(true).Foreground(t.Text()).Render(" opcode")
	plain := " opcode"
	if config.Get() != nil {
		where := " · " + shortHome(config.WorkingDirectory())
		left += muted.Render(where)
		plain += where
		if branch := p.gitBranch(); branch != "" {
			left += muted.Render(" · ") + bar.Foreground(t.Success()).Render(branch)
			plain += " · " + branch
		}
	}
	right := ""
	if title := strings.TrimSpace(p.session.Title); title != "" {
		right = ansi.Truncate(title, max(0, p.width-ansi.StringWidth(plain)-4), "…") + " "
	}
	gap := max(1, p.width-ansi.StringWidth(plain)-ansi.StringWidth(right))
	line := left + bar.Render(strings.Repeat(" ", gap)) + muted.Render(right)
	return ansi.Truncate(line, max(1, p.width), "…")
}

func shortHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}

// gitBranch reads .git/HEAD directly (following a worktree's gitdir file);
// it is re-read at most every few seconds, never by running git.
func (p *chatPage) gitBranch() string {
	if time.Since(p.branchRead) < 3*time.Second {
		return p.branch
	}
	p.branchRead = time.Now()
	p.branch = readGitBranch(config.WorkingDirectory())
	return p.branch
}

func readGitBranch(dir string) string {
	gitPath := filepath.Join(dir, ".git")
	if raw, err := os.ReadFile(gitPath); err == nil {
		target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
		if !ok {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		gitPath = target
	}
	raw, err := os.ReadFile(filepath.Join(gitPath, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if ref, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return ref
	}
	if len(head) >= 7 {
		return head[:7]
	}
	return ""
}
