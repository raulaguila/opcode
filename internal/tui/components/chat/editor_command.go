package chat

import (
	"fmt"
	"strings"
	"unicode"
)

// Parse executable and literal arguments without invoking a shell, expansion,
// substitutions or redirections. Quoted paths and EDITOR="code --wait" work.
func editorArguments(value string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	flush := func() {
		if started {
			args = append(args, word.String())
			word.Reset()
			started = false
		}
	}
	editorArgumentsValue(value, &word, &quote, &escaped, &started, flush)
	if quote != 0 || escaped {
		return nil, fmt.Errorf("EDITOR: incomplete quote or escape")
	}
	flush()
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("EDITOR: missing executable")
	}
	return args, nil
}

func editorArgumentsValue(value string, word *strings.Builder, quote *rune, escaped *bool, started *bool, flush func()) {
	for _, r := range value {
		if *escaped {
			word.WriteRune(r)
			(*escaped) = false
			(*started) = true
			continue
		}
		if r == '\\' && (*quote) != '\'' {
			(*escaped) = true
			(*started) = true
			continue
		}
		if (*quote) != 0 {
			consumeQuotedArgument(word, quote, r)
			continue
		}
		if r == '\'' || r == '"' {
			(*quote) = r
			(*started) = true
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		word.WriteRune(r)
		(*started) = true
	}
}

func consumeQuotedArgument(word *strings.Builder, quote *rune, value rune) {
	if value == *quote {
		*quote = 0
		return
	}
	word.WriteRune(value)
}
