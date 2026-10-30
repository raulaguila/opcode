package dialog

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type OpenIntegrationDialogMsg struct{}
type CloseIntegrationDialogMsg struct{}
type IntegrationSettingsSavedMsg struct{ Kind string }
type integrationProbeMsg struct {
	request uint64
	lines   []string
	err     error
}
type IntegrationDialog interface {
	tea.Model
	layout.Bindings
	Open() tea.Cmd
	Close()
}
type integrationDialogCmp struct {
	arguments *argumentList
	providerDialogCmp
	entries      []config.Integration
	current      config.Integration
	probe        func(context.Context, config.Integration) ([]string, error)
	result       []string
	resultOffset int
	oauth        OAuthHandler
	oauthEvents  chan integrationOAuthMsg
}

func NewIntegrationDialogCmp(probe func(context.Context, config.Integration) ([]string, error), allowSave ...func() error) IntegrationDialog {
	m := &integrationDialogCmp{probe: probe}
	if len(allowSave) > 0 {
		m.allowSave = allowSave[0]
	}
	return m
}
func (m *integrationDialogCmp) Open() tea.Cmd {
	m.Close()
	m.arguments = nil
	m.screen = "list"
	m.note = config.Text("N MCP · L LSP · Enter edita · Ctrl+A login · Ctrl+D logout · Ctrl+U status", "N MCP · L LSP · Enter edit · Ctrl+A login · Ctrl+D logout · Ctrl+U status")
	m.entries = nil
	m.selected = 0
	c := config.Get()
	if c == nil {
		return nil
	}
	for name, server := range c.MCPServers {
		m.entries = append(m.entries, config.Integration{Kind: "mcp", Name: name, MCP: server})
	}
	for name, server := range c.LSP {
		m.entries = append(m.entries, config.Integration{Kind: "lsp", Name: name, LSP: server})
	}
	slices.SortFunc(m.entries, func(a, b config.Integration) int { return strings.Compare(a.Kind+a.Name, b.Kind+b.Name) })
	return nil
}
func (m *integrationDialogCmp) edit(candidate config.Integration) tea.Cmd {
	m.Close()
	m.screen = "edit"
	m.current = candidate
	m.originalID = candidate.Name
	m.result = nil
	m.note = "Tab fields · Ctrl+T test with permission · Ctrl+S save · Esc back"

	if candidate.Kind == "mcp" {
		s := candidate.MCP
		if s.Type == "" {
			s.Type = config.MCPStdio
		}
		oauth := config.MCPOAuthConfig{}
		if s.OAuth != nil {
			oauth = *s.OAuth
		}
		m.setFields([]string{"Name", "Transport (stdio/http/sse)", "Command (stdio)", "Arguments (JSON array)", "URL (http/sse)", "Headers from environment (JSON header:ENV)", "Child environment (JSON target:ENV)", "Disabled (true/false)", "OAuth enabled (true/false)", "OAuth client ID (empty: dynamic registration)", "OAuth client secret environment variable", "OAuth scopes (space separated)", "OAuth redirect port (0: automatic)"}, []string{candidate.Name, string(s.Type), s.Command, integrationJSON(s.Args), s.URL, integrationJSON(s.HeadersEnv), integrationJSON(s.EnvVars), strconv.FormatBool(s.Disabled), strconv.FormatBool(s.OAuth != nil), oauth.ClientID, oauth.ClientSecretEnv, strings.Join(oauth.Scopes, " "), strconv.Itoa(oauth.RedirectPort)})
	} else {
		s := candidate.LSP
		m.setFields([]string{"Language/name", "Command", "Arguments (JSON array)", "Disabled (true/false)"}, []string{candidate.Name, s.Command, integrationJSON(s.Args), strconv.FormatBool(s.Disabled)})
	}
	return textinput.Blink
}
func (m *integrationDialogCmp) read() (config.Integration, error) {
	c := m.current
	// JSON decoding reuses non-nil maps/slices. Candidate edits must never
	// change an already registered connection before Save succeeds.
	c.MCP.Args = nil
	c.MCP.HeadersEnv = nil
	c.MCP.EnvVars = nil
	c.LSP.Args = nil
	c.Name = m.fieldValue(0)
	if m.originalID != "" && c.Name != m.originalID {
		return c, fmt.Errorf("name cannot be changed; add a new integration instead")
	}
	if err := readIntegrationFields(&c, m.fieldValue); err != nil {
		return c, err
	}

	return c, config.ValidateIntegration(c)
}
func (m *integrationDialogCmp) testIntegration() tea.Cmd {
	if m.probe == nil {
		m.note = "Connection testing is unavailable."
		return nil
	}
	candidate, err := m.read()
	if err != nil {
		m.note = err.Error()
		return nil
	}
	m.Close()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.testing = true
	request := m.request
	probe := m.probe
	return func() tea.Msg {
		lines, err := probe(ctx, candidate)
		cancel()
		return integrationProbeMsg{request: request, lines: lines, err: err}
	}
}
func (m *integrationDialogCmp) saveIntegration() tea.Cmd {
	if m.testing {
		m.note = "Wait for the test or press Esc to cancel before saving."
		return nil
	}
	if m.allowSave != nil {
		if err := m.allowSave(); err != nil {
			m.note = err.Error()
			return nil
		}
	}
	candidate, err := m.read()
	if err == nil {
		err = config.SaveIntegration(candidate)
	}
	if err != nil {
		m.note = err.Error()
		return nil
	}
	m.Close()
	return util.CmdHandler(IntegrationSettingsSavedMsg{Kind: candidate.Kind})
}
func (m *integrationDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.noteUpdate(msg) {
		return m, nil
	}
	if m.arguments != nil {
		return m, m.updateArgumentEditor(msg)
	}
	switch msg := msg.(type) {
	case integrationOAuthMsg:
		return m, m.updateOAuth(msg)
	case integrationProbeMsg:
		m.finishIntegrationProbe(msg)
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		action := m.handleIntegrationKey(msg)
		if action.handled {
			return m, action.command
		}
	case tea.MouseMsg:
		action := m.handleIntegrationMouse(msg)
		if action.handled {
			return m, action.command
		}
	}
	return m, m.updateIntegrationInput(msg)
}

func (m *integrationDialogCmp) View() string {
	if m.noteFull {
		return m.noteView()
	}
	if m.arguments != nil {
		return m.arguments.view(m.contentWidth(), m.height)
	}
	if m.screen == "oauth" {
		return m.oauthView()
	}
	t := theme.CurrentTheme()
	width := m.contentWidth()
	base := styles.BaseStyle().Width(width)
	line := func(s string) string {
		s = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, s)
		return base.Render(ansi.Truncate(interfaceCopy(s), width, "…"))
	}
	title := "MCP and language servers"
	if m.screen == "edit" {
		title = config.Text("Configurar ", "Configure ") + strings.ToUpper(m.current.Kind)
	}
	if m.screen == "result" {
		title = "Connection test results"
	}
	lines := []string{base.Foreground(t.Primary()).Bold(true).Render(ansi.Truncate(interfaceCopy(title), width, "…"))}
	lines = append(lines, m.integrationBodyLines(t, width, base, line)...)
	note := m.note
	if m.screen == "list" {
		note = config.Text("N Adicionar MCP · L Adicionar LSP · Enter Editar · Esc", "N Add MCP · L Add LSP · Enter Edit · Esc")
		if len(m.entries) > 0 && m.entries[m.selected].Kind == "mcp" && m.entries[m.selected].MCP.OAuth != nil {
			note += " · Ctrl+A Login · Ctrl+D Logout · Ctrl+U Status"
		}
	}
	if m.testing {
		note = "Testing… approve the connection request if prompted; Esc cancels."
	}
	if m.screen == "edit" && m.dirty() {
		lines = append(lines, line(config.Text("Alterações não salvas", "Unsaved changes")))
	}
	lines = append(lines, m.noteLines(note)...)
	return styles.BaseStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(t.TextMuted()).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *integrationDialogCmp) visibleFields() []int {
	if m.current.Kind != "mcp" || len(m.fields) < 13 {
		r := []int{}
		for i := range m.fields {
			r = append(r, i)
		}
		return r
	}
	r := []int{0, 1}
	if m.fields[1].Value() == "stdio" {
		r = append(r, 2, 3, 6, 7)
	} else {
		r = append(r, 4, 5, 7, 8)
		if m.fields[8].Value() == "true" {
			r = append(r, 9, 10, 11, 12)
		}
	}
	return r
}
