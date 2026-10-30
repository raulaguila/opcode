package tools

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/raulaguila/opcode/internal/config"
)

// OpcodeWorkspace is only a routing hint for first-party capability questions.
// It grants no access and never exposes manifest contents to the model.
func OpcodeWorkspace(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	root, err := os.OpenRoot(config.WorkingDirectory())
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Lstat("go.mod")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return false
	}
	f, err := root.Open("go.mod")
	if err != nil {
		return false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 16<<10+1))
	if err != nil || len(raw) > 16<<10 || ctx.Err() != nil {
		return false
	}
	matched := opcodeModuleManifest(raw)
	return ctx.Err() == nil && matched
}

func opcodeModuleManifest(raw []byte) bool {
	var tokens scanner.Scanner
	tokens.Init(strings.NewReader(string(raw)))
	tokens.Mode = scanner.ScanIdents | scanner.ScanStrings | scanner.ScanComments | scanner.SkipComments
	tokens.Error = func(*scanner.Scanner, string) {}
	for token := tokens.Scan(); token != scanner.EOF; token = tokens.Scan() {
		if tokens.TokenText() != "module" {
			continue
		}
		line := tokens.Position.Line
		var path strings.Builder
		for token = tokens.Scan(); token != scanner.EOF && tokens.Position.Line == line; token = tokens.Scan() {
			path.WriteString(tokens.TokenText())
		}
		name := path.String()
		if strings.HasPrefix(name, `"`) {
			unquoted, err := strconv.Unquote(name)
			if err != nil {
				return false
			}
			name = unquoted
		}
		return tokens.ErrorCount == 0 && name == "github.com/raulaguila/opcode"
	}
	return false
}
