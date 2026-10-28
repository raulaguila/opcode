package shell

import (
	"context"
	"testing"
)

func TestWorktreeCommandRejectsArbitraryGitAndEscapes(t *testing.T) {
	for _, args := range [][]string{{"reset", "--hard"}, {"remove", "--force", "/workspace/.opcode-worktrees/demo"}, {"remove", "--", "/workspace"}, {"add", "--detach", "--", "/outside/demo", "HEAD"}, {"add", "--detach", "--", "/workspace/.opcode-worktrees/demo", "-f"}} {
		if _, err := WorktreeCommand(context.Background(), "/workspace", args); err == nil {
			t.Fatalf("unsafe worktree arguments accepted: %q", args)
		}
	}
}
