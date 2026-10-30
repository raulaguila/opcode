package tools

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/raulaguila/opcode/internal/config"
)

var projectSourcePath = regexp.MustCompile(`[[:alnum:]_./-]+\.[[:alpha:]][[:alnum:]_]*`)

// ProjectSourcePaths only identifies literal relative source locators. The
// caller still checks regular files, sensitive paths and the read policy.
func ProjectSourcePaths(question string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, path := range projectSourcePath.FindAllString(question, 8) {
		if filepath.IsAbs(path) {
			if relative, err := filepath.Rel(config.WorkingDirectory(), path); err == nil {
				path = relative
			}
		}
		if filepath.IsLocal(path) && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

// ProjectEvidenceQuery routes a few repository topics to the existing bounded
// index. It is a search hint, never evidence, an instruction, or an approval.
func ProjectEvidenceQuery(question string) string {
	s := strings.ToLower(question)
	for _, topic := range []struct {
		terms []string
		query string
	}{
		{[]string{"linguagen", "linguagem", "languages", "language support"}, "language languages linguagens parser support"},
		{[]string{"arquitetura", "architecture", "estrutura do projeto", "project structure"}, "architecture arquitetura structure components"},
		{[]string{"permissões", "permissoes", "permissions", "sandbox"}, "permissions permission sandbox approval"},
		{[]string{"contexto do projeto", "contexto dos modelos", "controle de contexto", "context budget"}, "context budget retrieval compact"},
	} {
		for _, term := range topic.terms {
			if strings.Contains(s, term) {
				return topic.query
			}
		}
	}
	return ""
}

func ProjectInquiry(question string) bool {
	s := strings.ToLower(question)
	project := false
	for _, term := range []string{"projeto", "reposit", "codebase", "this project", "current project", "this repo", "opcode"} {
		project = project || strings.Contains(s, term)
	}
	if !project && ProjectEvidenceQuery(question) == "" {
		return false
	}
	if strings.Contains(s, "?") {
		return true
	}
	words := strings.FieldsFunc(strings.ReplaceAll(s, "don't", "dont"), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return projectQuestionWords(words)
}

func projectQuestionWords(words []string) bool {
	for i, word := range words {
		if i > 0 {
			switch words[i-1] {
			case "não", "nao", "nunca", "not", "dont", "never":
				continue
			}
		}
		if word == "o" && i+1 < len(words) && words[i+1] == "que" {
			return true
		}
		switch word {
		case "qual", "quais", "falar", "fale", "descreva", "explique", "analise", "what", "which", "how", "describe", "explain", "overview", "possui", "suporta", "supports":
			return true
		}
	}
	return false
}
