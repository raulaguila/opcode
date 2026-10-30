package styles

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

const defaultMargin = 1

//go:fix inline
func uintPtr(u uint) *uint { return new(u) }

// returns a glamour TermRenderer configured with the current theme
func GetMarkdownRenderer(width int) *glamour.TermRenderer {
	r, _ := glamour.NewTermRenderer(
		glamour.WithStyles(generateMarkdownStyleConfig()),
		glamour.WithWordWrap(width),
		glamour.WithColorProfile(lipgloss.ColorProfile()),
	)
	return r
}

// creates an ansi.StyleConfig for markdown rendering
// using adaptive colors from the provided theme.
func generateMarkdownStyleConfig() ansi.StyleConfig {
	t := theme.CurrentTheme()

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockPrefix: "",
				BlockSuffix: "",
				Color:       new(adaptiveColorToString(t.MarkdownText())),
			},
			Margin: uintPtr(defaultMargin),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:  new(adaptiveColorToString(t.MarkdownBlockQuote())),
				Italic: new(true),
				Prefix: "┃ ",
			},
			Indent:      uintPtr(1),
			IndentToken: new(BaseStyle().Render(" ")),
		},
		List: ansi.StyleList{
			LevelIndent: defaultMargin,
			StyleBlock: ansi.StyleBlock{
				IndentToken: new(BaseStyle().Render(" ")),
				StylePrimitive: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.MarkdownText())),
				},
			},
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       new(adaptiveColorToString(t.MarkdownHeading())),
				Bold:        new(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "# ",
				Color:  new(adaptiveColorToString(t.MarkdownHeading())),
				Bold:   new(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Color:  new(adaptiveColorToString(t.MarkdownHeading())),
				Bold:   new(true),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Color:  new(adaptiveColorToString(t.MarkdownHeading())),
				Bold:   new(true),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
				Color:  new(adaptiveColorToString(t.MarkdownHeading())),
				Bold:   new(true),
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
				Color:  new(adaptiveColorToString(t.MarkdownHeading())),
				Bold:   new(true),
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  new(adaptiveColorToString(t.MarkdownHeading())),
				Bold:   new(true),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: new(true),
			Color:      new(adaptiveColorToString(t.TextMuted())),
		},
		Emph: ansi.StylePrimitive{
			Color:  new(adaptiveColorToString(t.MarkdownEmph())),
			Italic: new(true),
		},
		Strong: ansi.StylePrimitive{
			Bold:  new(true),
			Color: new(adaptiveColorToString(t.MarkdownStrong())),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  new(adaptiveColorToString(t.MarkdownHorizontalRule())),
			Format: "\n─────────────────────────────────────────\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
			Color:       new(adaptiveColorToString(t.MarkdownListItem())),
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
			Color:       new(adaptiveColorToString(t.MarkdownListEnumeration())),
		},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     new(adaptiveColorToString(t.MarkdownLink())),
			Underline: new(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: new(adaptiveColorToString(t.MarkdownLinkText())),
			Bold:  new(true),
		},
		Image: ansi.StylePrimitive{
			Color:     new(adaptiveColorToString(t.MarkdownImage())),
			Underline: new(true),
			Format:    "🖼 {{.text}}",
		},
		ImageText: ansi.StylePrimitive{
			Color:  new(adaptiveColorToString(t.MarkdownImageText())),
			Format: "{{.text}}",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:  new(adaptiveColorToString(t.MarkdownCode())),
				Prefix: "",
				Suffix: "",
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Prefix: " ",
					Color:  new(adaptiveColorToString(t.MarkdownCodeBlock())),
				},
				Margin: uintPtr(defaultMargin),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.MarkdownText())),
				},
				Error: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.Error())),
				},
				Comment: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxComment())),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxKeyword())),
				},
				Keyword: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxKeyword())),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxKeyword())),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxKeyword())),
				},
				KeywordType: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxType())),
				},
				Operator: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxOperator())),
				},
				Punctuation: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxPunctuation())),
				},
				Name: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxVariable())),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxVariable())),
				},
				NameTag: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxKeyword())),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxFunction())),
				},
				NameClass: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxType())),
				},
				NameConstant: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxVariable())),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxFunction())),
				},
				NameFunction: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxFunction())),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxNumber())),
				},
				LiteralString: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxString())),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.SyntaxKeyword())),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.DiffRemoved())),
				},
				GenericEmph: ansi.StylePrimitive{
					Color:  new(adaptiveColorToString(t.MarkdownEmph())),
					Italic: new(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.DiffAdded())),
				},
				GenericStrong: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.MarkdownStrong())),
					Bold:  new(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: new(adaptiveColorToString(t.MarkdownHeading())),
				},
			},
		},
		Table: ansi.StyleTable{
			// The table renderer supplies block spacing. Prefix/suffix here also
			// apply to cells and would hide headers behind an initial blank line.
			CenterSeparator: new("┼"),
			ColumnSeparator: new("│"),
			RowSeparator:    new("─"),
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n ❯ ",
			Color:       new(adaptiveColorToString(t.MarkdownLinkText())),
		},
		Text: ansi.StylePrimitive{
			Color: new(adaptiveColorToString(t.MarkdownText())),
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(adaptiveColorToString(t.MarkdownText())),
			},
		},
	}
}

// adaptiveColorToString converts a lipgloss.AdaptiveColor to the appropriate
// hex color string based on the current terminal background
func adaptiveColorToString(color lipgloss.AdaptiveColor) string {
	if lipgloss.HasDarkBackground() {
		return color.Dark
	}
	return color.Light
}
