package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/lsp"
)

type LanguageServerState struct {
	Name                string `json:"name"`
	ExecutableAvailable bool   `json:"executable_available"`
	Disabled            bool   `json:"disabled"`
	StartedByInventory  bool   `json:"started_by_inventory"`
}

type LanguageServerSuggestion struct {
	Command             string   `json:"command"`
	Args                []string `json:"args"`
	ExecutableAvailable bool     `json:"executable_available"`
	RootMarkers         []string `json:"rootMarkers"`
}

type LanguageSupportEntry struct {
	Language           string                     `json:"language"`
	Recognition        bool                       `json:"recognition"`
	LocalParser        bool                       `json:"local_parser"`
	ParserFixture      bool                       `json:"parser_fixture"`
	SemanticReferences string                     `json:"semantic_references"`
	ConfiguredServers  []LanguageServerState      `json:"configured_servers"`
	ConfiguredChecks   []string                   `json:"configured_checks"`
	SuggestedServers   []LanguageServerSuggestion `json:"suggested_servers"`
	ProjectFiles       int                        `json:"project_files"`
}

type LanguageSupportReport struct {
	Languages        []LanguageSupportEntry `json:"languages"`
	ProjectInspected bool                   `json:"project_inspected"`
	FilesInspected   int                    `json:"files_inspected"`
	Truncated        bool                   `json:"truncated"`
	Note             string                 `json:"note"`
}

func canonicalServerLanguage(language string) string {
	switch language {
	case "typescriptreact":
		return "tsx"
	case "javascriptreact":
		return "javascript"
	case "shellscript":
		return "bash"
	case "terraform":
		return "hcl"
	}
	return language
}

func languageServerMatches(language string, types []string) bool {
	if language == "text" {
		return false
	}
	for _, typ := range types {
		typ = strings.TrimPrefix(strings.ToLower(typ), ".")
		if language == canonicalServerLanguage(typ) || language == sourceLanguage("file."+typ) {
			return true
		}
	}
	return false
}

var languageServerArgs = map[string][]string{
	"pyright-langserver": {"--stdio"}, "typescript-language-server": {"--stdio"},
	"intelephense": {"--stdio"}, "phpactor": {"language-server"},
	"bash-language-server": {"start"}, "dart": {"language-server", "--protocol=lsp"},
	"vue-language-server": {"--stdio"}, "svelteserver": {"--stdio"},
	"terraform-ls": {"serve"}, "yaml-language-server": {"--stdio"}, "taplo": {"lsp", "stdio"},
	"vscode-json-language-server": {"--stdio"}, "vscode-html-language-server": {"--stdio"}, "vscode-css-language-server": {"--stdio"},
}

// InspectLanguageSupport only discovers paths and executable availability. It
// never reads project source bodies, starts LSP, installs tools or invokes checks.
// The optional project scan uses the same exclusions and hard cap as discovery.
func InspectLanguageSupport(ctx context.Context, detectProject bool) (LanguageSupportReport, error) {
	report := LanguageSupportReport{Languages: []LanguageSupportEntry{}, ProjectInspected: detectProject,
		Note: "Syntax extraction is not semantic resolution. Checks are project-configured names, not proof of language coverage. Suggestions require explicit user configuration and installed servers; nothing is started or installed."}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	languages := map[string]int{}
	registered := map[string]LanguageSupportEntry{}
	for _, entry := range RegisteredLanguageSupport() {
		languages[entry.Language] = 0
		registered[entry.Language] = entry
	}
	if detectProject {
		root := config.WorkingDirectory()
		paths, truncated, err := findFiles(ctx, root, "", 2000)
		if err != nil {
			return report, err
		}
		report.Truncated = truncated
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			if retrievalPathAllowed(root, path, nil) {
				languages[sourceLanguage(path)]++
				report.FilesInspected++
			}
		}
	}
	names := make([]string, 0, len(languages))
	for name := range languages {
		names = append(names, name)
	}
	sort.Strings(names)
	cfg := config.Get()
	for _, name := range names {
		report.Languages = append(report.Languages, languageSupportEntry(name, languages[name], registered[name].ParserFixture, cfg))
	}
	return report, nil
}

type languageSupportTool struct{}

func NewLanguageSupportTool() BaseTool { return &languageSupportTool{} }
func (*languageSupportTool) Info() ToolInfo {
	return ToolInfo{Name: "get_language_support", Description: "Paged inventory of tested local syntax parsers, configured/available LSP servers, setup suggestions and configured check names. Optional bounded project discovery counts languages without reading file bodies. No server, installation or check is started.", Parameters: map[string]any{
		"language": map[string]any{"type": "string", "description": "Optional exact language or alias."},
		"offset":   map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
		"detect_project": map[string]any{"type": "boolean", "description": "Count project languages using shared exclusions; at most 2000 files."},
		"summary":        map[string]any{"type": "boolean", "description": "Compact Opcode parser inventory: profile_count and paged language names only; no project scan or executable discovery. Cannot combine with detect_project."},
	}}
}

func (*languageSupportTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var p languageInventoryParams
	if call.Input != "" && (json.Unmarshal([]byte(call.Input), &p) != nil || p.Offset < 0 || p.Limit < 0 || len(p.Language) > 80) {
		return NewTextErrorResponse("invalid language inventory parameters"), nil
	}
	if p.Summary {
		return languageInventorySummary(ctx, p)
	}
	report, err := InspectLanguageSupport(ctx, p.Detect)
	if err != nil {
		return ToolResponse{}, err
	}
	items := report.Languages
	if p.Language != "" {
		scope := retrievalScope{Language: p.Language}
		if err := scope.validate(); err != nil {
			return NewTextErrorResponse("invalid language filter"), nil
		}
		items = slices.DeleteFunc(items, func(entry LanguageSupportEntry) bool { return entry.Language != scope.Language })
	}
	limit := p.Limit
	if limit == 0 {
		limit = 10
	}
	start := min(p.Offset, len(items))
	end := min(start+min(limit, 20), len(items))
	return jsonToolResult(map[string]any{"profile_count": len(RegisteredLanguageSupport()), "languages": items[start:end], "total": len(items), "next_offset": end, "more": end < len(items), "project_inspected": report.ProjectInspected, "files_inspected": report.FilesInspected, "truncated": report.Truncated, "note": report.Note,
		"other_languages": "text search and optional configured LSP", "parser": fmt.Sprintf("Go standard parser and %d selected pure Go gotreesitter grammars", len(localParsers))}), nil
}

func languageSupportEntry(name string, files int, fixture bool, cfg *config.Config) LanguageSupportEntry {
	entry := LanguageSupportEntry{Language: name, Recognition: name != "text", LocalParser: name == "go" || localParsers[name] != nil,
		SemanticReferences: "configured LSP only", ConfiguredServers: configuredLanguageServers(name, cfg), ConfiguredChecks: []string{}, SuggestedServers: suggestedLanguageServers(name), ProjectFiles: files, ParserFixture: fixture}
	if cfg != nil {
		for check := range cfg.Checks {
			entry.ConfiguredChecks = append(entry.ConfiguredChecks, check)
		}
	}
	sort.Slice(entry.ConfiguredServers, func(i, j int) bool { return entry.ConfiguredServers[i].Name < entry.ConfiguredServers[j].Name })
	sort.Strings(entry.ConfiguredChecks)
	return entry
}
func configuredLanguageServers(name string, cfg *config.Config) []LanguageServerState {
	servers := []LanguageServerState{}
	if cfg == nil {
		return servers
	}
	for key, server := range cfg.LSP {
		types := server.FileTypes
		if len(types) == 0 {
			types = lsp.Presets[filepath.Base(server.Command)].Languages
		}
		if languageServerMatches(name, types) {
			_, err := exec.LookPath(server.Command)
			servers = append(servers, LanguageServerState{Name: key, Disabled: server.Disabled, ExecutableAvailable: err == nil})
		}
	}
	return servers
}
func suggestedLanguageServers(name string) []LanguageServerSuggestion {
	servers := []string{}
	for command, preset := range lsp.Presets {
		if languageServerMatches(name, preset.Languages) {
			servers = append(servers, command)
		}
	}
	sort.Strings(servers)
	suggestions := []LanguageServerSuggestion{}
	for _, command := range servers[:min(2, len(servers))] {
		_, err := exec.LookPath(command)
		args := slices.Clone(languageServerArgs[command])
		if args == nil {
			args = []string{}
		}
		suggestions = append(suggestions, LanguageServerSuggestion{Command: command, Args: args, ExecutableAvailable: err == nil, RootMarkers: slices.Clone(lsp.Presets[command].RootMarkers)})
	}
	return suggestions
}

type languageInventoryParams struct {
	Language string `json:"language"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
	Detect   bool   `json:"detect_project"`
	Summary  bool   `json:"summary"`
}

func languageInventorySummary(ctx context.Context, p languageInventoryParams) (ToolResponse, error) {
	if err := ctx.Err(); err != nil {
		return ToolResponse{}, err
	}
	if p.Detect {
		return NewTextErrorResponse("summary inventories Opcode parsers only; use a separate call for detect_project"), nil
	}
	if p.Language != "" {
		return NewTextErrorResponse("summary returns paged profile names; use the full inventory for language filtering"), nil
	}
	names := []string{}
	for _, entry := range RegisteredLanguageSupport() {
		names = append(names, entry.Language)
	}
	limit := p.Limit
	if limit == 0 {
		limit = 10
	}
	start := min(p.Offset, len(names))
	end := min(start+min(limit, 20), len(names))
	return jsonToolResult(map[string]any{"scope": "opcode_parser_capabilities", "profile_count": len(names), "supported_languages": names[start:end], "more": end < len(names), "next_offset": end, "note": "Includes the standard Go parser. This inventories Opcode tooling, not a third-party project's features. Detected project file types are not parser profiles."}), nil
}
