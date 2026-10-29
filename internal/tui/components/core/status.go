package core

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/lsp/protocol"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type StatusCmp interface {
	tea.Model
	ViewFor(util.FooterState) string
	ActionAt(int, util.FooterState) string
}
type statusCmp struct {
	enhancedKeyboard bool
	info             util.InfoMsg
	width            int
	messageTTL       time.Duration
	lspClients       map[string]*lsp.Client
}

func (m statusCmp) Init() tea.Cmd { return nil }
func (m statusCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case util.EnhancedKeyboardMsg:
		m.enhancedKeyboard = msg.Supported
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case util.InfoMsg:
		m.info = msg
		ttl := msg.TTL
		if ttl == 0 {
			ttl = m.messageTTL
		}
		return m, tea.Tick(ttl, func(time.Time) tea.Msg { return util.ClearStatusMsg{} })
	case util.ClearStatusMsg:
		m.info = util.InfoMsg{}
	}
	return m, nil
}
func (m statusCmp) View() string { return m.ViewFor(util.FooterState{}) }
func (m statusCmp) labels(state util.FooterState) (left, center, right string) {
	mode := state.Mode
	if mode == "" && config.Get() != nil {
		mode = config.Get().Runtime.Defaults().Mode
	}
	switch mode {
	case "ask":
		left = config.Text("Perguntar", "Ask")
	case "plan":
		left = config.Text("Planejar", "Plan")
	default:
		left = config.Text("Executar", "Execute")
	}
	left += " ▾"
	right = config.Text("Escolher modelo", "Choose model")
	if cfg := config.Get(); cfg != nil {
		if model, ok := models.SupportedModels[cfg.Agents[config.AgentCoder].Model]; ok {
			right = model.APIModel
			if right == "" {
				right = model.Name
			}
		}
	}
	right += " ▾"
	center = config.Text("/ comandos · Alt+Enter nova linha", "/ commands · Alt+Enter new line")
	if m.enhancedKeyboard {
		center = config.Text("/ comandos · Shift+Enter nova linha", "/ commands · Shift+Enter new line")
	}
	if state.Terminal {
		center = config.Shortcut("terminal") + config.Text(" conversa", " chat")
	} else if state.Activity != "" {
		center = state.Activity
		if state.Busy {
			center += config.Text(" · Esc cancela", " · Esc cancels")
		}
	}
	if state.Pending > 0 {
		center = fmt.Sprintf(config.Text("%d na fila · ", "%d queued · "), state.Pending) + center
	}
	if diagnostics := m.projectDiagnostics(); diagnostics != "" && !state.Busy {
		center = diagnostics + " · " + center
	}
	if m.info.Msg != "" && (state.Activity == "" && !state.Busy || m.info.Type == util.InfoTypeWarn || m.info.Type == util.InfoTypeError) {
		center = strings.Join(strings.Fields(m.info.Msg), " ")
		if m.info.Type == util.InfoTypeWarn {
			center = config.Text("Aviso: ", "Warning: ") + center
		}
		if m.info.Type == util.InfoTypeError {
			center = config.Text("Erro: ", "Error: ") + center
		}
	}
	return
}

// Rendering and mouse hits use the same width allocation.
func (m statusCmp) widths() (left, center, right int) {
	// Leave the final terminal cell blank. After a full-width write, an erase
	// command can otherwise clear the last glyph while autowrap is pending.
	width := max(0, m.width-1)
	left = min(12, width/3)
	right = min(28, width/3)
	center = width - left - right
	return
}

func compactTokens(n int64) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
	if n >= 1024 {
		if n%1024 == 0 {
			return fmt.Sprintf("%dk", n/1024)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1024)
	}
	return fmt.Sprint(n)
}
func (m statusCmp) metrics(state util.FooterState) string {
	if m.width < 72 || state.ContextWindow <= 0 {
		return ""
	}
	used := "—"
	if state.ContextKnown {
		used = compactTokens(state.ContextTokens)
		if state.ContextEstimated {
			used = "~" + used
		}
	}
	text := used + "/" + compactTokens(state.ContextWindow) + " ctx"
	if m.width >= 110 {
		rate := "—"
		if !state.Busy && state.TokensPerSecond > 0 && !math.IsNaN(state.TokensPerSecond) && !math.IsInf(state.TokensPerSecond, 0) {
			rate = fmt.Sprintf("%.1f", state.TokensPerSecond)
			if state.TokensPerSecond > 9999 {
				rate = fmt.Sprintf("%.3g", state.TokensPerSecond)
			}
			if state.SpeedEstimated {
				rate = "~" + rate
			}
		}
		text += " · " + rate + " tok/s"
	}
	return text
}
func (m statusCmp) metricWidths(state util.FooterState) (lw, cw, mw, rw int) {
	lw, cw, rw = m.widths()
	mw = min(max(0, cw-12), ansi.StringWidth(m.metrics(state))+2)
	if m.metrics(state) == "" {
		mw = 0
	}
	cw -= mw
	return
}
func (m statusCmp) ViewFor(state util.FooterState) string {
	left, center, right := m.labels(state)
	lw, cw, mw, rw := m.metricWidths(state)
	t := theme.CurrentTheme()
	segment := func(text string, width int, accent, alignRight bool) string {
		if width == 0 {
			return ""
		}
		style := styles.BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.Text()).Width(width).MaxWidth(width)
		if alignRight {
			style = style.Align(lipgloss.Right)
		}
		if accent {
			style = style.Foreground(t.Text())
			if width >= 4 {
				return style.Render(ansi.Truncate(" "+strings.TrimSuffix(text, " ▾"), width-2, "…") + " ▾")
			}
		}
		return style.Render(ansi.Truncate(" "+text, width, "…"))
	}
	middle := segment(center, cw, false, false)
	if m.info.Msg != "" && (m.info.Type == util.InfoTypeWarn || m.info.Type == util.InfoTypeError) {
		color := t.Warning()
		if m.info.Type == util.InfoTypeError {
			color = t.Error()
		}
		middle = styles.BaseStyle().Foreground(color).Background(t.BackgroundSecondary()).Width(cw).MaxWidth(cw).Render(ansi.Truncate(" "+center, cw, "…"))
	}
	view := lipgloss.JoinHorizontal(lipgloss.Top, segment(left, lw, true, false), middle, segment(m.metrics(state), mw, false, true), segment(right, rw, true, true))
	if m.width > 0 {
		view += styles.BaseStyle().Background(t.BackgroundSecondary()).Render(" ")
	}
	return view
}
func (m statusCmp) ActionAt(x int, state util.FooterState) string {
	lw, cw, mw, rw := m.metricWidths(state)
	if x < 0 || x >= lw+cw+mw+rw {
		return ""
	}
	if x < lw {
		return "mode"
	}
	if x >= lw+cw+mw {
		return "models"
	}
	if x >= lw+cw {
		return "context"
	}
	if m.info.Msg != "" && (state.Activity == "" && !state.Busy || m.info.Type == util.InfoTypeWarn || m.info.Type == util.InfoTypeError) {
		// Only open known panels. A hint can never execute a tool or retry work.
		for _, action := range []string{"integrations", "context", "settings", "models", "error"} {
			if strings.Contains(m.info.Msg, "/"+action) {
				return action
			}
		}
	}
	if state.Pending > 0 {
		return "queue"
	}
	return "commands"
}
func (m statusCmp) projectDiagnostics() string {
	errors, warnings := 0, 0
	for _, client := range m.lspClients {
		for _, file := range client.GetDiagnostics() {
			for _, diagnostic := range file {
				switch diagnostic.Severity {
				case protocol.SeverityError:
					errors++
				case protocol.SeverityWarning:
					warnings++
				}
			}
		}
	}
	var parts []string
	if errors > 0 {
		parts = append(parts, fmt.Sprintf(config.Text("%d erro(s)", "%d error(s)"), errors))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf(config.Text("%d aviso(s)", "%d warning(s)"), warnings))
	}
	return strings.Join(parts, " · ")
}
func NewStatusCmp(lspClients map[string]*lsp.Client) StatusCmp {
	return &statusCmp{messageTTL: 5 * time.Second, lspClients: lspClients}
}
