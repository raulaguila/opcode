package page

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

func (p *chatPage) planPanelRows() []string {
	rows := p.planRows()
	// Session identity and the full task controls remain in /task.
	if len(rows) <= 2 {
		return []string{config.Text("Descreva a tarefa para organizar as etapas.", "Describe the task to organize its steps.")}
	}
	return rows[1 : len(rows)-1]
}

func (p *chatPage) planPanelHeight() int {
	return min(5+min(len(p.planPanelRows()), max(1, min(8, p.height/3))), max(1, p.height-5))
}

func (p *chatPage) planVisibleRows() int {
	height := p.planPanelHeight()
	if height < 5 {
		return max(0, height-1)
	}
	if height < 7 {
		return height - 3
	}
	return height - 5
}

func (p *chatPage) planOffset() int {
	rows, offset := p.planPanelRows(), p.workflow.offset
	if offset < 0 {
		offset = 0
		for i, row := range rows {
			if strings.HasPrefix(row, " → ") {
				offset = i
				break
			}
		}
	}
	return min(max(0, offset), max(0, len(rows)-p.planVisibleRows()))
}

func (p *chatPage) planPanelTitle() string {
	title := config.Text("Plano", "Plan")
	if len(p.workflow.task.Steps) > 0 {
		done := 0
		for _, step := range p.workflow.task.Steps {
			if step.Status == "completed" {
				done++
			}
		}
		title += fmt.Sprintf(" %d/%d", done, len(p.workflow.task.Steps))
	}
	return title
}

func (p *chatPage) planPanelView(height int) string {
	width := max(1, p.width-4)
	if height < 5 {
		width = max(1, p.width)
	}
	rows := []string{styles.PanelTitle().Render(ansi.Truncate(p.planPanelTitle(), width, "…"))}
	body, offset, limit := p.planPanelRows(), p.planOffset(), p.planVisibleRows()
	t := theme.CurrentTheme()
	for _, row := range body[offset : offset+min(limit, len(body)-offset)] {
		style := styles.BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.Text())
		if strings.HasPrefix(row, " → ") {
			style = styles.SelectedRow().Bold(true)
		}
		rows = append(rows, style.Width(width).Render(ansi.Truncate(strings.TrimPrefix(row, " "), width, "…")))
	}
	for len(rows) < limit+1 {
		rows = append(rows, "")
	}
	if height >= 7 {
		rows = append(rows, styles.PanelDivider(width), styles.PanelHint().Render(ansi.Truncate(config.Text("Alt+↑/↓ rola · /task detalhes", "Alt+↑/↓ scroll · /task details"), width, "…")))
	}
	if height < 5 {
		return strings.Join(rows, "\n")
	}
	return styles.Panel().Padding(0, 1).Width(max(1, p.width)).Render(strings.Join(rows, "\n"))
}
