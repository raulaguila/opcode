package dialog

import (
	"context"
	"encoding/json"
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
	encode := func(v any) string {
		raw, _ := json.Marshal(v)
		if string(raw) == "null" {
			switch v.(type) {
			case []string:
				return "[]"
			default:
				return "{}"
			}
		}
		return string(raw)
	}
	if candidate.Kind == "mcp" {
		s := candidate.MCP
		if s.Type == "" {
			s.Type = config.MCPStdio
		}
		oauth := config.MCPOAuthConfig{}
		if s.OAuth != nil {
			oauth = *s.OAuth
		}
		m.setFields([]string{"Name", "Transport (stdio/http/sse)", "Command (stdio)", "Arguments (JSON array)", "URL (http/sse)", "Headers from environment (JSON header:ENV)", "Child environment (JSON target:ENV)", "Disabled (true/false)", "OAuth enabled (true/false)", "OAuth client ID (empty: dynamic registration)", "OAuth client secret environment variable", "OAuth scopes (space separated)", "OAuth redirect port (0: automatic)"}, []string{candidate.Name, string(s.Type), s.Command, encode(s.Args), s.URL, encode(s.HeadersEnv), encode(s.EnvVars), strconv.FormatBool(s.Disabled), strconv.FormatBool(s.OAuth != nil), oauth.ClientID, oauth.ClientSecretEnv, strings.Join(oauth.Scopes, " "), strconv.Itoa(oauth.RedirectPort)})
	} else {
		s := candidate.LSP
		m.setFields([]string{"Language/name", "Command", "Arguments (JSON array)", "Disabled (true/false)"}, []string{candidate.Name, s.Command, encode(s.Args), strconv.FormatBool(s.Disabled)})
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
	value := func(i int) string { return strings.TrimSpace(m.fields[i].Value()) }
	c.Name = value(0)
	if m.originalID != "" && c.Name != m.originalID {
		return c, fmt.Errorf("name cannot be changed; add a new integration instead")
	}
	var err error
	if c.Kind == "mcp" {
		c.MCP.Type = config.MCPType(value(1))
		c.MCP.Command = value(2)
		c.MCP.URL = value(4)
		if json.Unmarshal([]byte(value(3)), &c.MCP.Args) != nil {
			return c, fmt.Errorf("arguments must be a JSON array of strings")
		}
		if json.Unmarshal([]byte(value(5)), &c.MCP.HeadersEnv) != nil {
			return c, fmt.Errorf("headers must be a JSON object mapping header to environment name")
		}
		if json.Unmarshal([]byte(value(6)), &c.MCP.EnvVars) != nil {
			return c, fmt.Errorf("environment must be a JSON object mapping target to source variable")
		}
		c.MCP.Disabled, err = strconv.ParseBool(value(7))
		if err == nil {
			enabled, e := strconv.ParseBool(value(8))
			if e != nil {
				return c, fmt.Errorf("OAuth enabled must be true or false")
			}
			c.MCP.OAuth = nil
			if enabled {
				port, e := strconv.Atoi(value(12))
				if e != nil {
					return c, fmt.Errorf("OAuth redirect port must be an integer")
				}
				c.MCP.OAuth = &config.MCPOAuthConfig{ClientID: value(9), ClientSecretEnv: value(10), Scopes: strings.Fields(value(11)), RedirectPort: port}
			}
		}
	} else {
		c.LSP.Command = value(1)
		if json.Unmarshal([]byte(value(2)), &c.LSP.Args) != nil {
			return c, fmt.Errorf("arguments must be a JSON array of strings")
		}
		c.LSP.Disabled, err = strconv.ParseBool(value(3))
	}
	if err != nil {
		return c, fmt.Errorf("disabled must be true or false")
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
	switch msg := msg.(type) {
	case integrationOAuthMsg:
		return m, m.updateOAuth(msg)
	case integrationProbeMsg:
		if msg.request != m.request {
			return m, nil
		}
		m.testing = false
		m.cancel = nil
		if msg.err != nil {
			m.note = config.Text("Falha no teste: ", "Test failed: ") + msg.err.Error()
		} else {
			m.result = msg.lines
			if len(m.result) > 1024 {
				m.result = m.result[:1024]
			}
			m.resultOffset = 0
			m.screen = "result"
			m.note = "↑↓ scroll results · Esc returns to the preserved form"
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "esc" {
			if m.screen == "oauth" {
				m.Close()
				return m, m.Open()
			}
			if m.testing {
				m.Close()
				m.note = "Test cancelled; edits are preserved."
				return m, nil
			}
			if m.screen == "result" {
				m.screen = "edit"
				m.note = "Tab fields · Ctrl+T test · Ctrl+S save · Esc back"
				return m, nil
			}
			if m.screen != "list" {
				return m, m.Open()
			}
			m.Close()
			return m, util.CmdHandler(CloseIntegrationDialogMsg{})
		}
		if m.screen == "result" {
			switch msg.String() {
			case "up":
				m.resultOffset = max(0, m.resultOffset-1)
			case "down":
				m.resultOffset = min(max(0, len(m.result)-1), m.resultOffset+1)
			}
			return m, nil
		}
		if m.screen == "oauth" {
			if msg.String() == "up" {
				m.resultOffset = max(0, m.resultOffset-1)
			}
			if msg.String() == "down" {
				m.resultOffset++
			}
			return m, nil
		}
		if m.screen == "list" {
			switch msg.String() {
			case "ctrl+a":
				return m, m.oauthAction("login")
			case "ctrl+d":
				return m, m.oauthAction("logout")
			case "ctrl+u":
				return m, m.oauthAction("status")
			case "up":
				if len(m.entries) > 0 {
					m.selected = (m.selected - 1 + len(m.entries)) % len(m.entries)
				}
			case "down":
				if len(m.entries) > 0 {
					m.selected = (m.selected + 1) % len(m.entries)
				}
			case "enter":
				if len(m.entries) > 0 {
					return m, m.edit(m.entries[m.selected])
				}
			case "n":
				return m, m.edit(config.Integration{Kind: "mcp", MCP: config.MCPServer{Type: config.MCPStdio}})
			case "l":
				return m, m.edit(config.Integration{Kind: "lsp"})
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+s":
			return m, m.saveIntegration()
		case "ctrl+t":
			return m, m.testIntegration()
		case "tab", "down", "enter", "up", "shift+tab":
			m.fields[m.focus].Blur()
			delta := 1
			if msg.String() == "up" || msg.String() == "shift+tab" {
				delta = -1
			}
			m.focus = (m.focus + delta + len(m.fields)) % len(m.fields)
			return m, m.fields[m.focus].Focus()
		}
	case tea.MouseMsg:
		if m.screen == "edit" && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X >= 3 && msg.X < m.contentWidth()+3 {
			count := m.formRows()
			start := max(0, m.focus-count+1)
			i := start + (msg.Y-3)/2
			if msg.Y >= 3 && msg.Y < 3+count*2 && i < len(m.fields) {
				m.fields[m.focus].Blur()
				m.focus = i
				return m, m.fields[i].Focus()
			}
		}
		if m.screen == "list" && len(m.entries) > 0 {
			if msg.Button == tea.MouseButtonWheelUp {
				m.selected = max(0, m.selected-1)
			}
			if msg.Button == tea.MouseButtonWheelDown {
				m.selected = min(len(m.entries)-1, m.selected+1)
			}
		}
		if m.screen == "result" {
			if msg.Button == tea.MouseButtonWheelUp {
				m.resultOffset = max(0, m.resultOffset-1)
			}
			if msg.Button == tea.MouseButtonWheelDown {
				m.resultOffset = min(max(0, len(m.result)-1), m.resultOffset+1)
			}
			return m, nil
		}
		if m.screen == "list" && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X >= 3 && msg.X < m.contentWidth()+3 {
			rows := m.listRows()
			start := max(0, m.selected-rows+1)
			i := msg.Y - 3 + start
			if msg.Y >= 3 && msg.Y < 3+rows && i >= 0 && i < len(m.entries) {
				m.selected = i
				return m, m.edit(m.entries[i])
			}
		}
	}
	if m.screen == "edit" && len(m.fields) > 0 {
		var cmd tea.Cmd
		m.fields[m.focus], cmd = m.fields[m.focus].Update(msg)
		if _, ok := msg.(tea.KeyMsg); ok && m.testing {
			m.Close()
			m.note = "Edited; test again for current settings."
		}
		return m, cmd
	}
	return m, nil
}
func (m *integrationDialogCmp) View() string {
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
	if m.screen == "list" {
		rows := m.listRows()
		start := max(0, m.selected-rows+1)
		if len(m.entries) == 0 {
			lines = append(lines, line("No integrations. N adds MCP; L adds a language server."))
		}
		for i := start; i < min(len(m.entries), start+rows); i++ {
			style := base
			if i == m.selected {
				style = style.Foreground(t.Background()).Background(t.Primary()).Bold(true)
			}
			lines = append(lines, style.Render(ansi.Truncate(config.IntegrationSummary(m.entries[i]), width, "…")))
		}
	} else if m.screen == "edit" {
		count := m.formRows()
		start := max(0, m.focus-count+1)
		for i := start; i < min(len(m.fields), start+count); i++ {
			label := m.labels[i]
			if i == m.focus {
				label = "› " + label
			}
			lines = append(lines, line(label))
			m.fields[i].Width = max(8, width-2)
			lines = append(lines, line(m.fields[i].View()))
		}
	} else {
		rows := max(1, m.height-7)
		if m.height == 0 {
			rows = 12
		}
		for i := m.resultOffset; i < min(len(m.result), m.resultOffset+rows); i++ {
			lines = append(lines, line(m.result[i]))
		}
	}
	note := m.note
	if m.testing {
		note = "Testing… approve the connection request if prompted; Esc cancels."
	}
	lines = append(lines, line(note))
	return styles.BaseStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(t.TextMuted()).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
