package core

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	util.Model
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
func (m statusCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
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
	left = m.modeLabel(state) + " ▾"
	right = config.Text("Escolher modelo", "Choose model")
	if m.compactIdle(state) {
		right = config.Text("Modelo", "Model")
	}
	labelsGet(&right)
	right += " ▾"
	center = config.Text("/ comandos · Alt+Enter nova linha", "/ commands · Alt+Enter new line")
	if m.enhancedKeyboard {
		center = config.Text("/ comandos · Shift+Enter nova linha", "/ commands · Shift+Enter new line")
	}
	if state.Terminal {
		center = config.Shortcut("terminal") + config.Text(" conversa", " chat")
	} else if state.Activity != "" || state.Busy {
		center = footerActivityLabel(state)
		if state.Busy && state.ElapsedKnown {
			center += " · " + footerElapsed(state.ElapsedSeconds)
		}
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
	labelsTypeUtilInfoTypeError(&m, &state, &center)
	return
}

// Rendering and mouse hits use the same width allocation.
func (m statusCmp) widths(state util.FooterState) (left, center, right int) {
	// Leave the final terminal cell blank. After a full-width write, an erase
	// command can otherwise clear the last glyph while autowrap is pending.
	width := max(0, m.width-1)
	mode, approval := m.controlWidths(state)
	left = mode + approval
	switch {
	case m.width >= 160:
		right = 28
	case m.width >= 110:
		right = 20
	case m.width >= 80:
		right = 12
	case m.width >= 60:
		right = 4
		if m.compactIdle(state) {
			right = 12
		}
	}
	right = min(right, max(0, width-left))
	center = width - left - right
	return
}

func (m statusCmp) metricWidths(state util.FooterState) (lw, cw, mw, rw int) {
	lw, cw, rw = m.widths(state)
	reserve := 20
	if m.compactIdle(state) {
		reserve = 10
	}
	mw = min(max(0, cw-reserve), ansi.StringWidth(m.metrics(state))+1)
	if m.metrics(state) == "" {
		mw = 0
	}
	cw -= mw
	return
}

func (m statusCmp) compactIdle(state util.FooterState) bool {
	return m.width >= 60 && m.width < 80 && !state.Busy && !state.Terminal && state.Pending == 0 && m.info.Msg == "" &&
		(state.Activity == "" || state.Activity == config.Text("Concluído", "Completed"))
}

func (m statusCmp) ViewFor(state util.FooterState) string {
	left, center, right := m.labels(state)
	_, cw, mw, rw := m.metricWidths(state)
	lw, aw := m.controlWidths(state)
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
				arrow := " ▾"
				if m.width < 60 {
					arrow = "▾"
				}
				return style.Render(ansi.Truncate(" "+strings.TrimSuffix(text, " ▾"), width-ansi.StringWidth(arrow), "…") + arrow)
			}
		}
		return style.Render(ansi.Truncate(" "+text, width, "…"))
	}
	middle := m.middleSegment(center, cw, state)
	view := lipgloss.JoinHorizontal(lipgloss.Top, segment(left, lw, true, false), segment(m.approvalLabel(state)+" ▾", aw, true, false), middle, m.metricsSegment(state, mw), segment(right, rw, true, true))
	if m.width > 0 {
		view += styles.BaseStyle().Background(t.BackgroundSecondary()).Render(" ")
	}
	return view
}

func (m statusCmp) middleSegment(text string, width int, state util.FooterState) string {
	if width == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	style := styles.BaseStyle().Background(t.BackgroundSecondary())
	if m.idleHint(state) {
		style = styles.PanelHint()
	}
	if m.info.Msg != "" && (m.info.Type == util.InfoTypeWarn || m.info.Type == util.InfoTypeError) {
		color := t.Warning()
		if m.info.Type == util.InfoTypeError {
			color = t.Error()
		}
		style = style.Foreground(color)
	}
	if state.Busy && !state.Terminal && !m.hasPriorityInfo() {
		text = compactFooterActivity(text, width-1, state)
	}
	return style.Width(width).MaxWidth(width).Render(ansi.Truncate(" "+text, width, "…"))
}

func (m statusCmp) idleHint(state util.FooterState) bool {
	return state.Activity == "" && !state.Busy && state.Pending == 0 && m.info.Msg == "" && m.projectDiagnostics() == ""
}
func (m statusCmp) ActionAt(x int, state util.FooterState) string {
	lw, cw, mw, rw := m.metricWidths(state)
	if x < 0 || x >= lw+cw+mw+rw {
		return ""
	}
	mode, _ := m.controlWidths(state)
	if x < mode {
		return "mode"
	}
	if x < lw {
		return "approval"
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

func labelsTypeUtilInfoTypeError(m *statusCmp, state *util.FooterState, center *string) {
	if m.info.Msg != "" && (state.Activity == "" && !state.Busy || m.info.Type == util.InfoTypeWarn || m.info.Type == util.InfoTypeError) {
		(*center) = strings.Join(strings.Fields(m.info.Msg), " ")
		if m.info.Type == util.InfoTypeWarn {
			(*center) = config.Text("Aviso: ", "Warning: ") + (*center)
		}
		if m.info.Type == util.InfoTypeError {
			(*center) = config.Text("Erro: ", "Error: ") + (*center)
		}
	}
}

func labelsGet(right *string) {
	if cfg := config.Get(); cfg != nil {
		if model, ok := models.SupportedModels[cfg.Agents[config.AgentCoder].Model]; ok {
			(*right) = model.APIModel
			if (*right) == "" {
				(*right) = model.Name
			}
		}
	}
}

func footerActivityLabel(state util.FooterState) string {
	if state.Activity != "" {
		return state.Activity
	}
	if state.AwaitingModel {
		return config.Text("Aguardando modelo…", "Awaiting model…")
	}
	if state.SpeedLive {
		return config.Text("Gerando…", "Generating…")
	}
	return config.Text("Trabalhando…", "Working…")
}
