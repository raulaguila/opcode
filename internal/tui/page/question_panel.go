package page

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

type questionContent struct {
	intro   []string
	choices []string
	input   string
	hint    string
	focus   int
}

func (p *chatPage) questionPanelHeight() int {
	limit := max(1, p.height-5)
	if p.workflow.details {
		return min(limit, 18)
	}
	c := p.questionPanelContent(max(1, p.width-4))
	height := 6 + len(c.intro) + len(c.choices)
	if c.input != "" {
		height++
	}
	return min(limit, height)
}

func questionWrapped(text string, width int) []string {
	return strings.Split(ansi.Wrap(panelText(text), width, ""), "\n")
}

func (p *chatPage) questionPanelContent(width int) questionContent {
	q := p.workflow.question
	index := p.workflow.questionIndex
	if len(q.Questions) > 0 && index >= len(q.Questions) {
		return p.questionReviewContent(width)
	}
	item := interaction.QuestionItem{Question: q.Question, Explanation: q.Explanation, Choices: q.Choices}
	if len(q.Questions) > 0 {
		item = q.Questions[index]
		item.Question = fmt.Sprintf("%d/%d · %s", index+1, len(q.Questions), item.Question)
	}
	c := questionContent{intro: questionWrapped(item.Question, width), focus: p.workflow.choice}
	if item.Explanation != "" {
		for _, line := range questionWrapped(item.Explanation, width) {
			c.intro = append(c.intro, styles.PanelHint().Render(line))
		}
	}
	c.choices = p.questionChoiceRows(item, width)
	input := p.questionAnswerField(item, width)
	if len(item.Choices) > 0 {
		c.choices = append(c.choices, input)
	} else {
		c.input = input
	}
	c.hint = p.questionAnswerHint(item, len(q.Questions) > 0)
	return c
}

func (p *chatPage) questionAnswerField(item interaction.QuestionItem, width int) string {
	t := theme.CurrentTheme()
	style := styles.BaseStyle().Background(t.BackgroundSecondary())
	p.workflow.answer.Prompt = config.Text("Resposta: ", "Answer: ")
	if len(item.Choices) > 0 {
		prefix := "  ○ "
		if p.customQuestionAnswer(len(item.Choices)) {
			prefix = "› ● "
			style = styles.SelectedRow()
		}
		p.workflow.answer.Prompt = prefix + config.Text("Outra resposta: ", "Other answer: ")
	}
	p.workflow.answer.SetWidth(max(1, width-lipgloss.Width(p.workflow.answer.Prompt)-1))
	p.workflow.answer.Placeholder = ""
	inputStyles := p.workflow.answer.Styles()
	inputStyles.Focused.Text, inputStyles.Focused.Prompt = style, style
	inputStyles.Blurred.Text, inputStyles.Blurred.Prompt = style, style
	p.workflow.answer.SetStyles(inputStyles)
	return style.Width(width).Render(ansi.Truncate(p.workflow.answer.View(), width, "…"))
}

func (p *chatPage) questionAnswerHint(item interaction.QuestionItem, batch bool) string {
	if len(item.Choices) > 0 && p.workflow.choice < 0 {
		return config.Text("↑↓ escolha ou escreva · F2 detalhes · Esc cancela", "↑↓ choose or type · F2 details · Esc cancel")
	}
	target := config.Text("opção", "choice")
	if p.customQuestionAnswer(len(item.Choices)) {
		target = config.Text("seu texto", "your text")
	} else if item.Type == "multi_choice" {
		target = config.Text("opções marcadas", "checked choices")
	}
	if batch {
		if item.Type == "multi_choice" && !p.customQuestionAnswer(len(item.Choices)) {
			return config.Text("Espaço marca · Enter próxima: opções · F2 detalhes · Esc", "Space select · Enter next: choices · F2 details · Esc")
		}
		return fmt.Sprintf(config.Text("Enter próxima: %s · Shift+Tab volta · F2 detalhes · Esc", "Enter next: %s · Shift+Tab back · F2 details · Esc"), target)
	}
	return fmt.Sprintf(config.Text("Enter envia %s · ↑↓ escolhe · F2 detalhes · Esc", "Enter sends %s · ↑↓ choose · F2 details · Esc"), target)
}

func (p *chatPage) questionChoiceRows(item interaction.QuestionItem, width int) []string {
	rows := make([]string, 0, len(item.Choices))
	for i, choice := range item.Choices {
		mark, cursor := "○", " "
		if item.Type == "multi_choice" {
			mark = "[ ]"
		}
		selected := !p.customQuestionAnswer(len(item.Choices)) && p.workflow.selectedChoices[i]
		if item.Type != "multi_choice" {
			selected = p.workflow.choice == i
		}
		if selected {
			mark = "●"
			if item.Type == "multi_choice" {
				mark = "[✓]"
			}
		}
		if p.workflow.choice == i {
			cursor = "›"
		}
		prefix := fmt.Sprintf("%s %s ", cursor, mark)
		choice = questionChoiceLabel(panelText(choice), max(0, width-ansi.StringWidth(prefix)), p.workflow.details)
		line := prefix + choice
		if cursor == "›" {
			line = styles.SelectedRow().Bold(true).PaddingRight(max(0, width-ansi.StringWidth(line))).Render(line)
		}
		rows = append(rows, line)
	}
	return rows
}

func questionChoiceLabel(choice string, width int, details bool) string {
	length := ansi.StringWidth(choice)
	if details || length <= width {
		return choice
	}
	if width < 3 {
		return ansi.Truncate(choice, width, "…")
	}
	head := (width - 1) / 2
	tail := width - 1 - head
	suffix := ansi.Cut(choice, length-tail, length)
	// A cut through a wide grapheme can retain its full cell width.
	for ansi.StringWidth(suffix) > tail {
		cluster, _ := ansi.FirstGraphemeCluster(suffix, ansi.GraphemeWidth)
		suffix = suffix[len(cluster):]
	}
	return ansi.Cut(choice, 0, head) + "…" + suffix
}

func (p *chatPage) questionReviewContent(width int) questionContent {
	c := questionContent{intro: []string{config.Text("Revisar respostas", "Review answers")}, focus: -1,
		hint: config.Text("Enter envia · Shift+Tab volta · F2 detalhes", "Enter send · Shift+Tab back · F2 details")}
	for i, item := range p.workflow.question.Questions {
		answer := p.workflow.batchAnswers[i]
		value := answer.Text
		if len(answer.Choices) > 0 {
			value = strings.Join(answer.Choices, ", ")
		}
		c.choices = append(c.choices, questionWrapped(fmt.Sprintf("%d. %s: %s", i+1, item.Question, value), width)...)
	}
	return c
}

// Keep the question, focused choice and answer visible; F2 exposes the full text.
func (c questionContent) visibleRows(height int, divider string) []string {
	if height <= 0 {
		return nil
	}
	tail := []string{}
	if c.input != "" && height >= 2 {
		tail = append(tail, c.input)
	}
	if height >= 3 {
		if height >= 5 && divider != "" {
			tail = append(tail, divider)
		}
		tail = append(tail, styles.PanelHint().Render(c.hint))
	}
	remaining := height - len(tail)
	gap := len(c.intro) > 0 && (len(c.choices) > 0 || c.input != "") && remaining > len(c.intro)+len(c.choices)
	if gap {
		remaining--
	}
	choiceCount := min(len(c.choices), remaining/2)
	introCount := min(len(c.intro), remaining-choiceCount)
	choiceCount = min(len(c.choices), remaining-introCount)
	rows := append([]string{}, c.intro[:introCount]...)
	if gap {
		rows = append(rows, "")
	}
	start := max(0, min(c.focus-choiceCount+1, len(c.choices)-choiceCount))
	rows = append(rows, c.choices[start:start+choiceCount]...)
	return append(rows, tail...)
}

func (p *chatPage) questionPanelView(height int) string {
	width := max(1, p.width-4)
	c := p.questionPanelContent(width)
	title := config.Text("Pergunta para você", "Question for you")
	if height < 5 {
		rows := append([]string{title}, c.visibleRows(height-1, "")...)
		for i := range rows {
			rows[i] = ansi.Truncate(rows[i], max(1, p.width), "…")
		}
		return strings.Join(rows, "\n")
	}
	rows := []string{styles.PanelTitle().Render(title)}
	bodyHeight := height - 2 - len(rows)
	if p.workflow.details {
		rows = append(rows, p.questionDetailsRows(c, width, bodyHeight)...)
	} else {
		rows = append(rows, c.visibleRows(bodyHeight, styles.PanelDivider(width))...)
	}
	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], width, "…")
	}
	return styles.Panel().Padding(0, 1).Width(max(1, p.width)).
		Render(strings.Join(rows, "\n"))
}

func (p *chatPage) questionDetailsRows(c questionContent, width, height int) []string {
	if height < 2 {
		return c.visibleRows(height, "")
	}
	full := append([]string{}, c.intro...)
	if len(full) > 0 && len(c.choices) > 0 {
		full = append(full, "")
	}
	for _, choice := range c.choices {
		full = append(full, ansi.Wrap(choice, width, ""))
	}
	p.workflow.detailView.SetWidth(width)
	tail := []string{styles.PanelHint().Render(config.Text("F2/Esc volta · ↑↓/PgUp/PgDn rola", "F2/Esc back · ↑↓/PgUp/PgDn scroll"))}
	if height >= 5 {
		tail = append([]string{styles.PanelDivider(width)}, tail...)
	}
	p.workflow.detailView.SetHeight(height - len(tail))
	p.workflow.detailView.SetContent(strings.Join(full, "\n"))
	return append(strings.Split(p.workflow.detailView.View(), "\n"), tail...)
}
