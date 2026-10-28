package tools

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type indexedChunk struct {
	Symbol     string
	Signature  string
	Kind       string
	Start, End int
	Content    string
}

var camelBoundary = regexp.MustCompile(`([\p{Ll}\d])(\p{Lu})`)
var acronymBoundary = regexp.MustCompile(`(\p{Lu})(\p{Lu}\p{Ll})`)

func codeTerms(text string) []string {
	expanded := acronymBoundary.ReplaceAllString(text, "$1 $2")
	expanded = camelBoundary.ReplaceAllString(expanded, "$1 $2")
	fields := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	seen := map[string]bool{}
	var terms []string
	for _, term := range append(fields(expanded), fields(text)...) {
		if !seen[term] {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	return terms
}

func codeQueryTerms(query string) []string {
	stop := map[string]bool{}
	for _, term := range strings.Fields("a as o os de da do das dos e em no na nos nas um uma para por que como onde qual quais the an of to in is are where how what this project projeto código code") {
		stop[term] = true
	}
	concepts := map[string][]string{"permissão": {"permission", "authorize", "access"}, "permissões": {"permission", "authorize", "access"}, "autenticação": {"authentication", "auth", "login"}, "usuário": {"user"}, "contexto": {"context"}, "configuração": {"config", "settings"}, "arquivo": {"file"}, "busca": {"search"}, "erro": {"error", "exception"}}
	var result []string
	for _, term := range codeTerms(query) {
		if !stop[term] {
			result = append(result, term)
			result = append(result, concepts[term]...)
		}
	}
	if len(result) == 0 {
		result = codeTerms(query)
	}
	return result
}

func compactPreview(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	end := limit - 3
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "..."
}

func codeChunks(path, text string) []indexedChunk {
	lines := strings.Split(text, "\n")
	var chunks []indexedChunk
	add := func(symbol, kind string, first, last int) {
		first = max(1, first)
		last = min(last, len(lines))
		for start := first; start <= last; {
			end, size := start, 0
			for end <= last && end-start < 80 && size+len(lines[end-1])+1 <= 8192 {
				size += len(lines[end-1]) + 1
				end++
			}
			if end == start {
				end++
			}
			content := strings.Join(lines[start-1:end-1], "\n")
			if len(content) > 8192 {
				content = compactPreview(content, 8192)
			}
			chunks = append(chunks, indexedChunk{Symbol: symbol, Kind: kind, Signature: compactPreview(lines[first-1], 240), Start: start, End: end - 1, Content: content})
			start = end
		}
	}
	if sourceLanguage(path) == "go" {
		if symbols, err := goOutline(path, []byte(text)); err == nil {
			for _, symbol := range symbols {
				add(symbol.Name, symbol.Kind, symbol.Start, symbol.End)
			}
		}
	}
	for start := 1; start <= len(lines); start += 60 {
		add("<file>", "text", start, min(start+59, len(lines)))
	}
	return chunks
}
