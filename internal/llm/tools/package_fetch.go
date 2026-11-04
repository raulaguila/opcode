package tools

import (
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
