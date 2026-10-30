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
	if err := readIntegrationFields(&c, value); err != nil {
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
	if result0, result1, handled := updateArguments(m, msg); handled {
		return result0, result1
	}

	switch msg := msg.(type) {
	case integrationOAuthMsg:
		return m, m.updateOAuth(msg)
	case integrationProbeMsg:
		return updateCaseIntegrationProbeMsg(m, &msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if result0, result1, handled := updateIntegrationDialogCmpKeyMsg(&m, &msg); handled {
			return result0, result1
		}
	case tea.MouseMsg:
		if result0, result1, handled := updateIntegrationDialogCmpMouseMsg(m, &msg); handled {
			return result0, result1
		}
	}
	if result0, result1, handled := updateEdit(m, msg); handled {
		return result0, result1
	}
	return m, nil
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
	viewListCase(m, t, width, &base, line, &lines)
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

func updateIntegrationDialogCmpKeyMsg(m **integrationDialogCmp, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if result0, result1, result2, handled := updateIntegrationDialogCmpKeyMsgEdit(m, msg); handled {
		return result0, result1, result2
	}
	if result0, result1, result2, handled := updateIntegrationDialogCmpKeyMsgEditCase(m, msg); handled {
		return result0, result1, result2
	}
	if result0, result1, result2, handled := updateIntegrationDialogCmpKeyMsgEsc(&m, msg); handled {
		return result0, result1, result2
	}
	if (*m).screen == "result" {
		switch msg.String() {
		case "up":
			(*m).resultOffset = max(0, (*m).resultOffset-1)
		case "down":
			(*m).resultOffset = min(max(0, len((*m).result)-1), (*m).resultOffset+1)
		}
		return (*m), nil, true
	}
	if (*m).screen == "oauth" {
		if msg.String() == "up" {
			(*m).resultOffset = max(0, (*m).resultOffset-1)
		}
		if msg.String() == "down" {
			(*m).resultOffset++
		}
		return (*m), nil, true
	}
	if result0, result1, result2, handled := updateIntegrationDialogCmpKeyMsgList(m, msg); handled {
		return result0, result1, result2
	}
	switch msg.String() {
	case "ctrl+s":
		return (*m), (*m).saveIntegration(), true
	case "ctrl+t":
		return (*m), (*m).testIntegration(), true
	case "tab", "down", "enter", "up", "shift+tab":
		return updateIntegrationDialogCmpKeyMsgTab(m, msg)
	}
	return extractedResult0, extractedResult1, false
}

func updateIntegrationDialogCmpMouseMsg(m *integrationDialogCmp, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if result0, result1, result2, handled := updateIntegrationDialogCmpMouseMsgEdit(m, msg); handled {
		return result0, result1, result2
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
		return m, nil, true
	}
	if result0, result1, result2, handled := updateIntegrationDialogCmpMouseMsgList(m, msg); handled {
		return result0, result1, result2
	}
	return extractedResult0, extractedResult1, false
}

func updateIntegrationDialogCmpKeyMsgTab(m **integrationDialogCmp, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	(*m).fields[(*m).focus].Blur()
	delta := 1
	if msg.String() == "up" || msg.String() == "shift+tab" {
		delta = -1
	}
	indices := (*m).visibleFields()
	index := slices.Index(indices, (*m).focus)
	(*m).focus = indices[(index+delta+len(indices))%len(indices)]
	return (*m), (*m).fields[(*m).focus].Focus(), true

}

func updateIntegrationDialogCmpKeyMsgEdit(m **integrationDialogCmp, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if (*m).screen == "edit" && msg.String() == "enter" && (strings.HasPrefix((*m).labels[(*m).focus], "Arguments") || strings.HasPrefix((*m).labels[(*m).focus], "Headers from") || strings.HasPrefix((*m).labels[(*m).focus], "Child environment")) {
		var err error
		if strings.HasPrefix((*m).labels[(*m).focus], "Arguments") {
			(*m).arguments, err = newArgumentList((*m).fields[(*m).focus].Value())
		} else {
			(*m).arguments, err = newPairList((*m).fields[(*m).focus].Value())
		}
		if err != nil {
			(*m).note = err.Error()
			return (*m), nil, true, true
		}
		(*m).Close()
		(*m).fields[(*m).focus].Blur()
		return (*m), textinput.Blink, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateIntegrationDialogCmpKeyMsgList(m **integrationDialogCmp, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if (*m).screen == "list" {
		switch msg.String() {
		case "ctrl+a":
			return (*m), (*m).oauthAction("login"), true, true
		case "ctrl+d":
			return (*m), (*m).oauthAction("logout"), true, true
		case "ctrl+u":
			return (*m), (*m).oauthAction("status"), true, true
		case "up":
			if len((*m).entries) > 0 {
				(*m).selected = ((*m).selected - 1 + len((*m).entries)) % len((*m).entries)
			}
		case "down":
			if len((*m).entries) > 0 {
				(*m).selected = ((*m).selected + 1) % len((*m).entries)
			}
		case "enter":
			if len((*m).entries) > 0 {
				return (*m), (*m).edit((*m).entries[(*m).selected]), true, true
			}
		case "r":
			if (*m).resumeDraft != nil {
				(*m).resumeDraft()
				return (*m), textinput.Blink, true, true
			}
		case "n":
			return (*m), (*m).edit(config.Integration{Kind: "mcp", MCP: config.MCPServer{Type: config.MCPStdio}}), true, true
		case "l":
			return (*m), (*m).edit(config.Integration{Kind: "lsp"}), true, true
		}
		return (*m), nil, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func viewListCase(m *integrationDialogCmp, t theme.Theme, width int, base *lipgloss.Style, line func(s string) string, lines *[]string) {
	if m.screen == "list" {
		rows := m.listRows()
		start := max(0, m.selected-rows+1)
		if len(m.entries) == 0 {
			(*lines) = append((*lines), line("No integrations. N adds MCP; L adds a language server."))
		}
		for i := start; i < min(len(m.entries), start+rows); i++ {
			style := (*base)
			if i == m.selected {
				style = style.Foreground(t.Background()).Background(t.Primary()).Bold(true)
			}
			(*lines) = append((*lines), style.Render(ansi.Truncate(selectionMark(i == m.selected)+config.IntegrationSummary(m.entries[i]), width, "…")))
		}
	} else if m.screen == "edit" {
		indices := m.visibleFields()
		count := min(m.formRows(), len(indices))
		start := max(0, slices.Index(indices, m.focus)-count+1)
		viewListCaseIndicesStartCount(m, width, base, line, lines, indices, count, start)
	} else {
		rows := max(1, m.height-7)
		if m.height == 0 {
			rows = 12
		}
		for i := m.resultOffset; i < min(len(m.result), m.resultOffset+rows); i++ {
			(*lines) = append((*lines), line(m.result[i]))
		}
	}
}

func updateArguments(m *integrationDialogCmp, msg tea.Msg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if m.arguments != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = size.Width, size.Height
		}
		done, save, cmd := m.arguments.update(msg)
		if done {
			if save {
				m.fields[m.focus].SetValue(m.arguments.value())
			}
			m.arguments = nil
			m.fields[m.focus].Focus()
		}
		return m, cmd, true
	}
	return extractedResult0, extractedResult1, false
}

func updateIntegrationDialogCmpKeyMsgEsc(m ***integrationDialogCmp, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if msg.String() == "esc" {
		if (*(*m)).screen == "oauth" {
			(*(*m)).Close()
			return (*(*m)), (*(*m)).Open(), true, true
		}
		if (*(*m)).testing {
			(*(*m)).Close()
			(*(*m)).note = "Test cancelled; edits are preserved."
			return (*(*m)), nil, true, true
		}
		if (*(*m)).screen == "result" {
			(*(*m)).screen = "edit"
			(*(*m)).note = "Tab fields · Ctrl+T test · Ctrl+S save · Esc back"
			return (*(*m)), nil, true, true
		}
		if (*(*m)).screen != "list" {
			saved := *(*(*m))
			saved.cancel = nil
			saved.testing = false
			saved.resumeDraft = nil
			cmd := (*(*m)).Open()
			(*(*m)).resumeDraft = func() {
				w, h, request := (*(*m)).width, (*(*m)).height, (*(*m)).request+1
				*(*(*m)) = saved
				(*(*m)).width, (*(*m)).height, (*(*m)).request = w, h, request
			}
			(*(*m)).note = config.Text("R retoma a edição preservada", "R resumes preserved edits")
			return (*(*m)), cmd, true, true
		}
		(*(*m)).Close()
		return (*(*m)), util.CmdHandler(CloseIntegrationDialogMsg{}), true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func readIntegrationFields(c *config.Integration, value func(int) string) error {
	if c.Kind == "mcp" {
		return readMCPFields(c, value)
	}
	c.LSP.Command = value(1)
	if json.Unmarshal([]byte(value(2)), &c.LSP.Args) != nil {
		return fmt.Errorf("arguments must be a JSON array of strings")
	}
	var err error
	c.LSP.Disabled, err = strconv.ParseBool(value(3))
	if err != nil {
		return fmt.Errorf("disabled must be true or false")
	}
	return nil
}
func readMCPFields(c *config.Integration, value func(int) string) error {
	c.MCP.Type = config.MCPType(value(1))
	c.MCP.Command = value(2)
	c.MCP.URL = value(4)
	if json.Unmarshal([]byte(value(3)), &c.MCP.Args) != nil {
		return fmt.Errorf("arguments must be a JSON array of strings")
	}
	if json.Unmarshal([]byte(value(5)), &c.MCP.HeadersEnv) != nil {
		return fmt.Errorf("headers must be a JSON object mapping header to environment name")
	}
	if json.Unmarshal([]byte(value(6)), &c.MCP.EnvVars) != nil {
		return fmt.Errorf("environment must be a JSON object mapping target to source variable")
	}
	var err error
	c.MCP.Disabled, err = strconv.ParseBool(value(7))
	if err != nil {
		return fmt.Errorf("disabled must be true or false")
	}
	return readMCPOAuthFields(c, value)
}
func readMCPOAuthFields(c *config.Integration, value func(int) string) error {
	enabled, err := strconv.ParseBool(value(8))
	if err != nil {
		return fmt.Errorf("OAuth enabled must be true or false")
	}
	c.MCP.OAuth = nil
	if !enabled {
		return nil
	}
	port, err := strconv.Atoi(value(12))
	if err != nil {
		return fmt.Errorf("OAuth redirect port must be an integer")
	}
	c.MCP.OAuth = &config.MCPOAuthConfig{ClientID: value(9), ClientSecretEnv: value(10), Scopes: strings.Fields(value(11)), RedirectPort: port}
	return nil
}

func viewListCaseIndicesStartCount(m *integrationDialogCmp, width int, base *lipgloss.Style, line func(s string) string, lines *[]string, indices []int, count int, start int) {
	for _, i := range indices[start:min(len(indices), start+count)] {
		label := fieldLabel(m.labels[i])
		if strings.HasPrefix(m.labels[i], "Arguments") {
			label = config.Text("Argumentos · Enter edita lista", "Arguments · Enter edits list")
		}
		if i == m.focus {
			label = "› " + label
		}
		(*lines) = append((*lines), line(label))
		m.fields[i].Width = max(8, width-2)
		(*lines) = append((*lines), base.Render(ansi.Truncate(choiceView(m.fields[i], fieldOptions(m.labels[i])), width, "…")))
	}
}

func updateIntegrationDialogCmpMouseMsgList(m *integrationDialogCmp, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if m.screen == "list" && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X >= 3 && msg.X < m.contentWidth()+3 {
		rows := m.listRows()
		start := max(0, m.selected-rows+1)
		i := msg.Y - 3 + start
		if msg.Y >= 3 && msg.Y < 3+rows && i >= 0 && i < len(m.entries) {
			m.selected = i
			return m, m.edit(m.entries[i]), true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateEdit(m *integrationDialogCmp, msg tea.Msg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if m.screen == "edit" && len(m.fields) > 0 {
		var cmd tea.Cmd
		m.fields[m.focus], cmd = m.fields[m.focus].Update(msg)
		if _, ok := msg.(tea.KeyMsg); ok && m.testing {
			m.Close()
			m.note = "Edited; test again for current settings."
		}
		return m, cmd, true
	}
	return extractedResult0, extractedResult1, false
}

func updateIntegrationDialogCmpKeyMsgEditCase(m **integrationDialogCmp, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if (*m).screen == "edit" && len((*m).fields) > (*m).focus && choiceKey(&(*m).fields[(*m).focus], fieldOptions((*m).labels[(*m).focus]), (*msg)) {
		if (*m).testing {
			(*m).Close()
			(*m).note = "Edited; test again for current settings."
		}
		return (*m), nil, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateIntegrationDialogCmpMouseMsgEdit(m *integrationDialogCmp, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if m.screen == "edit" && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X >= 3 && msg.X < m.contentWidth()+3 {
		indices := m.visibleFields()
		count := min(m.formRows(), len(indices))
		start := max(0, slices.Index(indices, m.focus)-count+1)
		i := start + (msg.Y-3)/2
		if msg.Y >= 3 && msg.Y < 3+count*2 && i < len(indices) {
			m.fields[m.focus].Blur()
			m.focus = indices[i]
			return m, m.fields[m.focus].Focus(), true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateCaseIntegrationProbeMsg(m *integrationDialogCmp, msg *integrationProbeMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd) {
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

}
