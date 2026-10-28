// Package extensions discovers explicitly configured skills. Skill text is
// untrusted guidance and never changes tool access, modes or permissions.
package extensions

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
)

const MaxSkillBytes = 16384

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
}

func readSkill(root, relative string) (Skill, string, error) {
	if !filepath.IsLocal(relative) || shell.SensitivePath(filepath.Join(root, relative)) {
		return Skill{}, "", fmt.Errorf("invalid skill path")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return Skill{}, "", err
	}
	defer r.Close()
	info, err := r.Lstat(relative)
	if err != nil {
		return Skill{}, "", err
	}
	if !info.Mode().IsRegular() {
		return Skill{}, "", fmt.Errorf("skill must be a regular file")
	}
	file, err := r.Open(relative)
	if err != nil {
		return Skill{}, "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, MaxSkillBytes+1))
	if err != nil {
		return Skill{}, "", err
	}
	if len(raw) > MaxSkillBytes || strings.ContainsRune(string(raw), 0) {
		return Skill{}, "", fmt.Errorf("skill exceeds 16 KiB or is not text")
	}
	text := string(raw)
	name := filepath.Base(filepath.Dir(relative))
	description := ""
	if strings.HasPrefix(text, "---\n") {
		parts := strings.SplitN(text, "\n---", 2)
		if len(parts) == 2 {
			for _, line := range strings.Split(parts[0], "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				value = strings.Trim(strings.TrimSpace(value), "\"'")
				switch key {
				case "name":
					name = value
				case "description":
					description = value
				}
			}
		}
	}
	if name == "." || name == "" || len(name) > 100 || strings.ContainsAny(name, " /\\\r\n\x00") {
		return Skill{}, "", fmt.Errorf("invalid skill name")
	}
	if len(description) > 400 {
		description = description[:400]
	}
	hash := sha256.Sum256(raw)
	return Skill{Name: name, Description: tools.RedactCredentials(description), Path: filepath.Join(root, relative), SHA256: fmt.Sprintf("%x", hash)}, tools.RedactCredentials(text), nil
}
func Discover(ctx context.Context, dirs []string) ([]Skill, error) {
	var result []Skill
	names := map[string]bool{}
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("skill directories must be absolute")
		}
		directory, err := os.Open(dir)
		if err != nil {
			return nil, fmt.Errorf("cannot open skill directory: %w", err)
		}
		entries, readErr := directory.ReadDir(257)
		closeErr := directory.Close()
		if readErr != nil && readErr != io.EOF {
			return nil, fmt.Errorf("cannot list skill directory: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("cannot close skill directory: %w", closeErr)
		}
		if len(entries) > 256 {
			return nil, fmt.Errorf("skill directory exceeds 256 entries")
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !entry.IsDir() {
				continue
			}
			skill, _, err := readSkill(dir, filepath.Join(entry.Name(), "SKILL.md"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if names[skill.Name] {
				return nil, fmt.Errorf("duplicate skill name: %s", skill.Name)
			}
			names[skill.Name] = true
			result = append(result, skill)
			if len(result) > 64 {
				return nil, fmt.Errorf("at most 64 configured skills are supported")
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
func Load(ctx context.Context, dirs []string, name string) (Skill, string, error) {
	all, err := Discover(ctx, dirs)
	if err != nil {
		return Skill{}, "", err
	}
	for _, item := range all {
		if item.Name == name {
			return readSkill(filepath.Dir(filepath.Dir(item.Path)), filepath.Join(filepath.Base(filepath.Dir(item.Path)), "SKILL.md"))
		}
	}
	return Skill{}, "", fmt.Errorf("configured skill not found")
}
