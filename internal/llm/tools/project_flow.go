package tools

import (
	"strings"
	"unicode"
)

// Flow hints only select bounded navigation; source reads remain the evidence.
func ProjectFlowQuery(question string) string {
	lower := strings.ToLower(question)
	if !flowRequestSubject(lower) || !containsFlowTerm(lower, []string{"analise", "análise", "analyze", "analyse", "trace", "explique", "explain", "como", "how", "qual", "which"}) {
		return ""
	}
	for _, denial := range []string{"não analise", "nao analise", "não explique", "nao explique", "don't explain", "do not explain", "don't analyze", "do not analyze"} {
		if strings.Contains(lower, denial) {
			return ""
		}
	}
	var terms []string
	for _, term := range codeQueryTerms(question) {
		switch term {
		case "quero", "analise", "análise", "analyze", "analyse", "trace", "explique", "explain", "fluxo", "flow", "pipeline", "chamada", "chamadas":
			continue
		}
		terms = append(terms, term)
		if len(terms) == 12 {
			break
		}
	}
	return strings.Join(terms, " ")
}

func containsFlowTerm(text string, terms []string) bool {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) })
	for _, word := range words {
		for _, term := range terms {
			if word == term {
				return true
			}
		}
	}
	return false
}

func flowRequestSubject(text string) bool {
	if containsFlowTerm(text, []string{"fluxo", "flow", "pipeline"}) {
		return true
	}
	for _, phrase := range []string{"caminho de execução", "caminho de execucao", "execution path", "call chain"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
