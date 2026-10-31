package tools

import (
	"regexp"
	"strings"
)

const reviewStopWords = " sugira sugere sugerir sugestão sugestao sugestões sugestoes melhoria melhorias melhorar próprio proprio atual opcode revisão revisao faça faca podemos fazer neste nesse este esse aqui for and suggest suggestions recommend recommendations review improvements improvement improved lógica logica logic reduce reduction redução reducao reduzir complexity complexidade refactor refactoring refatoração refatoracao legibilidade readability analise análise analyze analyse projeto project code código repository repo repositório repositorio "

var negatedProjectReview = regexp.MustCompile(`(?i)(?:não|nao|nunca|don't|do not|never)\s+(?:(?:quero|precisa|want|need|faça|faca)\s+)?(?:que\s+)?(?:uma\s+)?(?:suggest|review|recommend|sugira|sugere|revisão|revisao|sugestões|sugestoes|analise|análise|analyze|analyse)\b`)

// ProjectReviewQuery selects bounded navigation for code review requests; it
// grants no execution or edit capabilities and does not establish a finding.
func ProjectReviewQuery(question string) string {
	lower := strings.ToLower(question)
	if !projectReviewIntent(lower) || !projectReviewSubject(question) || negatedProjectReview.MatchString(lower) {
		return ""
	}
	var terms []string
	// Preserve identifier spelling so existing exact-symbol ranking still works.
	for _, token := range strings.Fields(question) {
		token = strings.Trim(token, "`(),;?!\"'")
		if codeIdentifier(token) && len(token) <= 160 && !strings.Contains(reviewStopWords, " "+strings.ToLower(token)+" ") {
			terms = append(terms, token)
		}
	}
	for _, term := range codeQueryTerms(question) {
		if !strings.Contains(reviewStopWords, " "+term+" ") {
			terms = append(terms, term)
		}
	}
	return boundedReviewTerms(terms)
}

func projectReviewIntent(lower string) bool {
	if containsFlowTerm(lower, []string{"review", "suggest", "recommend", "suggestions", "sugira", "sugere", "sugestão", "sugestao", "sugestões", "sugestoes", "revisão", "revisao", "analise", "análise", "analyze", "analyse"}) {
		return true
	}
	return containsFlowTerm(lower, []string{"melhorias", "melhorar", "improvements", "improved"}) && containsFlowTerm(lower, []string{"qual", "quais", "pode", "podemos", "what", "which", "where"})
}

func projectReviewSubject(question string) bool {
	if containsFlowTerm(strings.ToLower(question), []string{"código", "codigo", "code", "codebase", "projeto", "opcode", "repository", "repo", "repositório", "repositorio"}) || strings.Contains(strings.ToLower(question), "this project") || strings.Contains(strings.ToLower(question), "current project") || len(ProjectSourcePaths(question)) > 0 {
		return true
	}
	for _, token := range strings.Fields(question) {
		if codeIdentifier(strings.Trim(token, "`(),;?!\"'")) {
			return true
		}
	}
	return false
}

func boundedReviewTerms(terms []string) string {
	var query strings.Builder
	seen := map[string]bool{}
	for _, term := range terms {
		key := strings.ToLower(term)
		if seen[key] || len(term) > 160 || query.Len()+len(term)+1 > 500 {
			continue
		}
		seen[key] = true
		if query.Len() > 0 {
			query.WriteByte(' ')
		}
		query.WriteString(term)
		if len(seen) == 12 {
			break
		}
	}
	if query.Len() == 0 {
		return "request run execute"
	}
	return query.String()
}
