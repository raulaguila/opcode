package agent

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/workflow"
)

func coreSchema(name string) bool {
	switch name {
	case "request_tools", "get_tools_catalog", "view", "search_code_index", "get_task", "get_context_status", "ask_user_question", "update_plan", "finish_task":
		return true
	}
	return false
}
func catalogParameters() map[string]any {
	str := map[string]any{"type": "string"}
	return map[string]any{"query": str, "category": str, "source": str, "read_only": map[string]any{"type": "boolean"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 16}}
}
func toolCategory(name string) string {
	switch {
	case strings.Contains(name, "mcp"):
		return "mcp"
	case strings.Contains(name, "skill"):
		return "skills"
	case strings.Contains(name, "context") || strings.Contains(name, "conversation") || strings.Contains(name, "finding"):
		return "memory"
	case strings.Contains(name, "process") || name == "bash" || strings.Contains(name, "check"):
		return "terminal"
	case strings.Contains(name, "task") || strings.Contains(name, "plan") || strings.Contains(name, "question"):
		return "workflow"
	case strings.HasPrefix(name, "git_"):
		return "git"
	case strings.Contains(name, "lsp") || name == "definition" || name == "references" || name == "diagnostics":
		return "language"
	default:
		return "files"
	}
}
func toolCatalog(run *generationRun, raw string) (any, error) {
	var in toolCatalogQuery
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, err
	}
	if len(in.Query) > 160 || in.Offset < 0 || in.Limit < 0 || in.Limit > 16 {
		return nil, fmt.Errorf("query max 160 bytes; offset >=0; limit 1–16")
	}
	if in.Limit == 0 {
		in.Limit = 8
	}
	items := toolCatalogEntries(run, in)
	slices.SortFunc(items, func(a, b toolCatalogEntry) int { return strings.Compare(a.Name, b.Name) })
	start := min(in.Offset, len(items))
	end := min(start+in.Limit, len(items))
	var next *int
	if end < len(items) {
		next = &end
	}
	return map[string]any{"items": items[start:end], "total": len(items), "next_offset": next}, nil
}
func taskProjection(t workflow.Task) map[string]any {
	pending := []workflow.Step{}
	for _, step := range t.Steps {
		if step.Status != "completed" && len(pending) < 3 {
			step.Title = clip(step.Title, 180)
			pending = append(pending, step)
		}
	}
	return map[string]any{"objective": clip(t.Objective, 600), "status": t.Status, "revision": t.Revision, "changed": t.Changed, "next_steps": pending, "criteria_count": len(t.Criteria), "checks": t.Checks, "details_tool": "get_task"}
}

type toolCatalogQuery struct {
	Query, Category, Source string
	ReadOnly                *bool `json:"read_only"`
	Offset, Limit           int
}
type toolCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"`
	ReadOnly    bool   `json:"read_only"`
	Loaded      bool   `json:"loaded"`
}

func toolCatalogEntries(run *generationRun, in toolCatalogQuery) []toolCatalogEntry {
	items := []toolCatalogEntry{}
	for _, t := range run.available {
		if !runAllowsTool(run, t) {
			continue
		}
		info := t.Info()
		source := "builtin"
		ro := readOnlyTool(info.Name) && !tools.IsOpaqueTool(t)
		if tools.IsOpaqueTool(t) {
			source = "mcp"
		}
		category := toolCategory(info.Name)
		if !in.matchesMetadata(category, source, ro) {
			continue
		}
		if in.Query != "" && !strings.Contains(strings.ToLower(info.Name+" "+info.Description), strings.ToLower(in.Query)) {
			continue
		}
		items = append(items, toolCatalogEntry{info.Name, clip(info.Description, 140), category, source, ro, run.selected[info.Name]})
	}

	return items
}

func (in toolCatalogQuery) matchesMetadata(category, source string, readOnly bool) bool {
	return (in.Category == "" || in.Category == category) && (in.Source == "" || in.Source == source) && (in.ReadOnly == nil || *in.ReadOnly == readOnly)
}
