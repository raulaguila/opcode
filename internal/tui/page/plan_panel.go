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
		return nil
	}
	return rows[1 : len(rows)-1]
}

func (p *chatPage) planPanelHeight() int {
	// Borders, the progress row and the visible steps.
	return min(max(5, 3+min(len(p.planPanelRows()), max(1, min(8, p.height/3)))), max(1, p.height-5))
}

func (p *chatPage) planVisibleRows() int {
	height := p.planPanelHeight()
	if height < 5 {
		return max(0, height-1)
	}
	return height - 3
}

func (p *chatPage) planOffset() int {
	rows, offset := p.planPanelRows(), p.workflow.offset
	if offset < 0 {
		offset = 0
		for i, row := range rows {
			if strings.HasPrefix(row, " ● ") {
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
		title += fmt.Sprintf(config.Text(" · %d de %d concluídas", " · %d of %d completed"), done, len(p.workflow.task.Steps))
	}
	return title
}

func (p *chatPage) planPanelView(height int) string {
	width := max(1, p.width-4)
	if height < 5 {
		width = max(1, p.width)
	}
	body, offset, limit := p.planPanelRows(), p.planOffset(), p.planVisibleRows()
	title := p.planPanelTitle()
	if offset > 0 {
		title += " ↑"
	}
	if offset+limit < len(body) {
		title += " ↓"
	}
	var rows []string
	if height < 5 {
		rows = append(rows, styles.PanelTitle().Render(ansi.Truncate(title, width, "…")))
	}
	t := theme.CurrentTheme()
	if height >= 5 {
		rows = append(rows, p.planProgressBar(width))
	}
	for _, row := range body[offset : offset+min(limit, len(body)-offset)] {
		style := styles.BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.Text())
		switch {
		case strings.HasPrefix(row, " ● "):
			style = styles.SelectedRow().Bold(true)
		case strings.HasPrefix(row, " ✓ "):
			// Done steps recede: muted and struck through, as a checklist.
			style = styles.PanelHint().Strikethrough(true)
		}
		rows = append(rows, style.Width(width).Render(ansi.Truncate(strings.TrimPrefix(row, " "), width, "…")))
	}
	if height < 5 {
		for len(rows) < limit+1 {
			rows = append(rows, "")
		}
		return strings.Join(rows, "\n")
	}
	for len(rows) < height-2 {
		rows = append(rows, "")
	}
	hint := config.Text("Alt+↑/↓ rola · /task detalhes", "Alt+↑/↓ scroll · /task details")
	// Title, mode and legend sit in the frame's borders.
	meta := modeLabel(p.mode()) + config.Text(" · tarefa salva", " · task saved")
	t = theme.CurrentTheme()
	return styles.FrameOn(strings.Join(rows, "\n"), width, 1, 0, title, meta, hint, t.Warning(), t.BackgroundSecondary())
}

// planProgressBar is a full-width bar of completed steps with the percentage.
func (p *chatPage) planProgressBar(width int) string {
	steps := p.workflow.task.Steps
	if len(steps) == 0 {
		return ""
	}
	done := 0
	for _, step := range steps {
		if step.Status == "completed" {
			done++
		}
	}
	label := fmt.Sprintf(" %d%%", done*100/len(steps))
	span := max(1, width-ansi.StringWidth(label))
	filled := done * span / len(steps)
	t := theme.CurrentTheme()
	return styles.BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.Warning()).Render(strings.Repeat("█", filled)) +
		styles.PanelHint().Render(strings.Repeat("░", span-filled)+label)
}

// planStrip is the collapsed plan: progress, the current step and the key
// that expands it, on one line.
func (p *chatPage) planStrip() string {
	steps := p.workflow.task.Steps
	if len(steps) == 0 || p.width < 40 {
		return ""
	}
	done, current := 0, ""
	for i, step := range steps {
		if step.Status == "completed" {
			done++
		}
		if step.Status == "in_progress" && current == "" {
			current = fmt.Sprintf("● %d. %s", i+1, panelText(step.Title))
		}
	}
	if current == "" && done < len(steps) {
		for i, step := range steps {
			if step.Status != "completed" {
				current = fmt.Sprintf("○ %d. %s", i+1, panelText(step.Title))
				break
			}
		}
	}
	t := theme.CurrentTheme()
	bar := styles.BaseStyle().Background(t.BackgroundSecondary())
	head := bar.Foreground(t.Warning()).Bold(true).Render(fmt.Sprintf(config.Text(" Plano %d/%d ", " Plan %d/%d "), done, len(steps)))
	tail := styles.PanelHint().Render(" /plan ")
	room := max(0, p.width-ansi.StringWidth(ansi.Strip(head))-ansi.StringWidth(ansi.Strip(tail)))
	middle := bar.Foreground(t.Text()).Render(ansi.Truncate(" "+current, room, "…"))
	gap := bar.Render(strings.Repeat(" ", max(0, room-ansi.StringWidth(ansi.Strip(middle)))))
	return head + middle + gap + tail
}
