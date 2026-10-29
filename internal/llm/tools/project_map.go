package tools

import (
	"context"
	"encoding/json"
	"github.com/pelletier/go-toml/v2"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func projectFileRole(path string) string {
	base := strings.ToLower(filepath.Base(path))
	lower := strings.ToLower(filepath.ToSlash(path))
	switch base {
	case "go.mod", "package.json", "pyproject.toml", "cargo.toml", "pom.xml", "build.gradle", "composer.json", "gemfile", "cmakelists.txt":
		return "manifest"
	}
	if strings.Contains(lower, "/test") || strings.HasPrefix(base, "test_") || strings.Contains(base, "_test.") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return "test"
	}
	if base == "main.go" || base == "main.py" || base == "__main__.py" || base == "main.rs" || base == "index.ts" || base == "index.js" || base == "program.cs" {
		return "entry_point"
	}
	if strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".rst") {
		return "documentation"
	}
	return "source"
}
func structuralProjectMap(ctx context.Context, path string, offset, limit int) (ToolResponse, error) {
	root, err := authorizeFile(ctx, path, "read")
	if err != nil {
		return ToolResponse{}, err
	}
	files, truncated, err := scopedFilePaths(ctx, root, retrievalScope{}, nil)
	if err != nil {
		return ToolResponse{}, err
	}
	snapshot, err := listingSnapshot(ctx, root, files)
	if err != nil {
		return ToolResponse{}, err
	}
	if limit <= 0 {
		limit = 30
	}
	limit = min(limit, 60)
	start := min(max(0, offset), len(files))
	end := start
	type item struct {
		Path         string       `json:"path"`
		Bytes        int64        `json:"bytes"`
		Language     string       `json:"language"`
		Role         string       `json:"role"`
		Package      string       `json:"package,omitempty"`
		Declarations []codeSymbol `json:"declarations,omitempty"`
		Dependencies []string     `json:"dependencies,omitempty"`
	}
	items := []item{}
	size := 0
	parseReads := 0
	for end < len(files) && end-start < limit {
		name := files[end]
		full := filepath.Join(root, filepath.FromSlash(name))
		info, e := os.Lstat(full)
		if e != nil {
			return ToolResponse{}, e
		}
		entry := item{Path: name, Bytes: info.Size(), Language: sourceLanguage(name), Role: projectFileRole(name)}
		if info.Size() <= 64<<10 && parseReads < 12 {
			raw, e := readFile(ctx, full)
			if e != nil {
				return ToolResponse{}, e
			}
			parseReads++
			if symbols, e := localOutline(ctx, name, raw); e == nil {
				entry.Declarations = symbols[:min(8, len(symbols))]
			}
			if entry.Language == "go" {
				if file, e := parser.ParseFile(token.NewFileSet(), name, raw, parser.ImportsOnly); e == nil {
					entry.Package = file.Name.Name
					for _, imp := range file.Imports[:min(12, len(file.Imports))] {
						v, _ := strconv.Unquote(imp.Path.Value)
						entry.Dependencies = append(entry.Dependencies, v)
					}
				}
			}
			if filepath.Base(name) == "package.json" {
				var manifest struct {
					Name            string
					Dependencies    map[string]json.RawMessage
					DevDependencies map[string]json.RawMessage
				}
				if json.Unmarshal(raw, &manifest) == nil {
					entry.Package = clipString(manifest.Name, 120)
					names := []string{}
					for dep := range manifest.Dependencies {
						names = append(names, dep)
					}
					sort.Strings(names)
					for _, dep := range names {
						entry.Dependencies = append(entry.Dependencies, clipString(dep, 120))
						if len(entry.Dependencies) == 12 {
							break
						}
					}
				}
			}
			if filepath.Base(name) == "pyproject.toml" || filepath.Base(name) == "Cargo.toml" {
				var manifest struct {
					Project struct {
						Name         string
						Dependencies []string
					}
					Package      struct{ Name string }
					Dependencies map[string]any
				}
				if toml.Unmarshal(raw, &manifest) == nil {
					entry.Package = manifest.Project.Name
					if entry.Package == "" {
						entry.Package = manifest.Package.Name
					}
					entry.Package = clipString(entry.Package, 120)
					for _, dep := range manifest.Project.Dependencies[:min(12, len(manifest.Project.Dependencies))] {
						entry.Dependencies = append(entry.Dependencies, clipString(dep, 120))
					}
					names := []string{}
					for dep := range manifest.Dependencies {
						names = append(names, dep)
					}
					sort.Strings(names)
					for _, dep := range names[:min(12, len(names))] {
						entry.Dependencies = append(entry.Dependencies, clipString(dep, 120))
					}
				}
			}
		}
		raw, _ := json.Marshal(entry)
		if size+len(raw) > 7500 {
			break
		}
		items = append(items, entry)
		size += len(raw)
		end++
	}
	return jsonToolResult(map[string]any{"root": root, "files": items, "total": len(files), "offset": start, "next_offset": end, "more": end < len(files), "truncated": truncated, "snapshot": snapshot, "navigation_only": true, "dependencies": "declared import/package names; not resolved transitive references", "parse_reads": parseReads}), nil
}
func clipString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
