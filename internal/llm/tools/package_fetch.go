package tools

import (
	"context"
	"slices"
	"strings"
)

// packageFetches restore what the project already declares; anything naming a
// package or URL could fetch code the project never chose, so it still asks.
var packageFetches = []string{
	"go mod download", "go mod tidy", "npm ci", "npm install", "pnpm install",
	"yarn install", "cargo fetch", "pip install -r requirements.txt",
}

func packageFetchCommand(command string) bool {
	return slices.Contains(packageFetches, strings.Join(strings.Fields(command), " ")) && !strings.ContainsAny(command, "\n\r")
}

// packageFetchDeclared says the worktree matches HEAD. The agent edits
// manifests, imports and install scripts without asking, so only committed
// content is what the project declares; any change, or no Git, asks first.
func packageFetchDeclared(ctx context.Context) bool {
	out, truncated, err := gitOutput(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	return err == nil && !truncated && out == ""
}
