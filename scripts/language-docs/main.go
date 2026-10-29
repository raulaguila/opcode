// Command language-docs renders the bundled runtime inventory without discovery.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/raulaguila/opcode/internal/llm/tools"
)

func main() {
	output := flag.String("output", "docs/language-support.md", "Generated Markdown file")
	check := flag.Bool("check", false, "Fail if the generated inventory differs from the saved file")
	flag.Parse()
	generated := []byte(tools.LanguageSupportMarkdown())
	if *check {
		current, err := os.ReadFile(*output)
		if err != nil || string(current) != string(generated) {
			fmt.Fprintln(os.Stderr, "language documentation drift; run go run ./scripts/language-docs")
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*output, generated, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
