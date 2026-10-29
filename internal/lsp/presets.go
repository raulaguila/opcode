package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
)

type ServerPreset struct {
	Languages   []string
	RootMarkers []string
}

var Presets = map[string]ServerPreset{
	"gopls":                       {[]string{"go"}, []string{"go.mod", "go.work"}},
	"pyright-langserver":          {[]string{"python"}, []string{"pyproject.toml", "setup.py", "requirements.txt"}},
	"pylsp":                       {[]string{"python"}, []string{"pyproject.toml", "setup.py"}},
	"typescript-language-server":  {[]string{"javascript", "javascriptreact", "typescript", "typescriptreact"}, []string{"package.json", "tsconfig.json"}},
	"rust-analyzer":               {[]string{"rust"}, []string{"Cargo.toml"}},
	"clangd":                      {[]string{"c", "cpp"}, []string{"compile_commands.json", "CMakeLists.txt"}},
	"jdtls":                       {[]string{"java"}, []string{"pom.xml", "build.gradle"}},
	"csharp-ls":                   {[]string{"csharp"}, []string{"global.json", "Directory.Build.props"}},
	"ruby-lsp":                    {[]string{"ruby"}, []string{"Gemfile", ".ruby-version"}},
	"solargraph":                  {[]string{"ruby"}, []string{"Gemfile", ".ruby-version"}},
	"intelephense":                {[]string{"php"}, []string{"composer.json"}},
	"phpactor":                    {[]string{"php"}, []string{"composer.json"}},
	"kotlin-language-server":      {[]string{"kotlin"}, []string{"settings.gradle.kts", "settings.gradle", "build.gradle.kts", "build.gradle", "pom.xml"}},
	"sourcekit-lsp":               {[]string{"swift", "c", "cpp"}, []string{"Package.swift", "compile_commands.json"}},
	"sqls":                        {[]string{"sql"}, []string{".sqls.yml", ".sqls.yaml"}},
	"bash-language-server":        {[]string{"shellscript"}, []string{".shellcheckrc", "Makefile", "package.json"}},
	"lua-language-server":         {[]string{"lua"}, []string{".luarc.json", ".luarc.jsonc"}},
	"dart":                        {[]string{"dart"}, []string{"pubspec.yaml"}},
	"vue-language-server":         {[]string{"vue"}, []string{"package.json", "tsconfig.json"}},
	"svelteserver":                {[]string{"svelte"}, []string{"package.json", "svelte.config.js", "svelte.config.ts"}},
	"terraform-ls":                {[]string{"terraform", "hcl"}, []string{".terraform.lock.hcl", "main.tf", "terragrunt.hcl"}},
	"yaml-language-server":        {[]string{"yaml"}, []string{".yamllint", ".yamllint.yml", "package.json"}},
	"taplo":                       {[]string{"toml"}, []string{"taplo.toml", ".taplo.toml", "Cargo.toml", "pyproject.toml"}},
	"vscode-json-language-server": {[]string{"json", "jsonc"}, []string{"package.json", "tsconfig.json"}},
	"vscode-html-language-server": {[]string{"html"}, []string{"package.json"}},
	"vscode-css-language-server":  {[]string{"css", "scss", "less"}, []string{"package.json"}},
}

func ServerMatches(c config.LSPConfig, path string) bool {
	preset := Presets[filepath.Base(c.Command)]
	types := c.FileTypes
	if len(types) == 0 {
		types = preset.Languages
	}
	if len(types) == 0 {
		return false
	}
	language := string(DetectLanguageID(path))
	if !slices.Contains(types, language) && !slices.Contains(types, strings.TrimPrefix(filepath.Ext(path), ".")) {
		return false
	}
	markers := c.RootMarkers
	if len(markers) == 0 {
		markers = preset.RootMarkers
	}
	if len(markers) == 0 {
		return true
	}
	root := config.WorkingDirectory()
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return false
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		for _, marker := range markers {
			if !filepath.IsLocal(marker) || len(marker) > 160 {
				continue
			}
			if info, err := os.Lstat(filepath.Join(dir, marker)); err == nil && info.Mode().IsRegular() {
				return true
			}
		}
		if dir == root || filepath.Dir(dir) == dir {
			break
		}
	}
	return false
}
