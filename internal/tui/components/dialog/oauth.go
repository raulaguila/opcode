package dialog

import (
	"context"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
)

type OAuthHandler func(context.Context, string, string, func(string) error) (string, error)
type integrationOAuthMsg struct {
	request     uint64
	url, status string
	err         error
	done        bool
}

func (m *integrationDialogCmp) SetOAuthHandler(handler OAuthHandler) { m.oauth = handler }
func (m *integrationDialogCmp) oauthAction(action string) tea.Cmd {
	if m.oauth == nil || len(m.entries) == 0 {
		return nil
	}
	entry := m.entries[min(m.selected, len(m.entries)-1)]
	if entry.Kind != "mcp" || entry.MCP.OAuth == nil {
		m.note = config.Text("Configure OAuth nesta conexão MCP primeiro.", "Configure OAuth on this MCP connection first.")
		return nil
	}
	m.Close()
	m.screen = "oauth"
	m.testing = true
	m.result = nil
	m.note = config.Text("Autenticação explícita · Esc cancela", "Explicit authentication · Esc cancels")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	m.cancel = cancel
	request := m.request
	events := make(chan integrationOAuthMsg, 2)
	handler := m.oauth
	go func() {
		defer cancel()
		status, err := handler(ctx, action, entry.Name, func(url string) error {
			select {
			case events <- integrationOAuthMsg{request: request, url: url}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		events <- integrationOAuthMsg{request: request, status: status, err: err, done: true}
	}()
	m.oauthEvents = events
	return m.waitOAuth()
}
func (m *integrationDialogCmp) waitOAuth() tea.Cmd {
	events := m.oauthEvents
	return func() tea.Msg { return <-events }
}
func (m *integrationDialogCmp) updateOAuth(msg integrationOAuthMsg) tea.Cmd {
	if msg.request != m.request {
		return nil
	}
	if msg.url != "" {
		m.result = []string{config.Text("Abra esta URL no seu navegador:", "Open this URL in your browser:"), msg.url}
		return m.waitOAuth()
	}
	m.testing = false
	m.cancel = nil
	if msg.err != nil {
		m.result = append(m.result, config.Text("Falha: ", "Failed: ")+msg.err.Error())
	} else {
		m.result = append(m.result, msg.status)
	}
	m.note = config.Text("Esc volta às conexões", "Esc returns to connections")
	return nil
}
func (m *integrationDialogCmp) oauthView() string {
	width := m.contentWidth()
	rows := max(1, m.height-10)
	all := []string{}
	for _, line := range m.result {
		line = ansi.Strip(line)
		line = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, line)
		all = append(all, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
	}
	if len(all) == 0 {
		all = append(all, config.Text("Aguardando servidor…", "Waiting for server…"))
	}
	m.resultOffset = min(m.resultOffset, max(0, len(all)-rows))
	title := config.Text("Autenticação MCP", "MCP authentication")
	legend := styles.PanelHint().Render(config.Text(" · ↑↓ rola", " · ↑↓ scrolls"))
	lines := []string{ansi.Truncate(title+legend, width, "…")}
	lines = append(lines, all[m.resultOffset:min(len(all), m.resultOffset+rows)]...)
	note := ansi.Truncate(m.note, width, "…")
	if !m.testing {
		note = dialogHint(m.note, width)
	}
	lines = append(lines, styles.PanelDivider(width), note)
	return styles.Panel().Padding(1, 2).Render(joinLines(lines))
}

func joinLines(lines []string) string {
	var out strings.Builder
	for i, s := range lines {
		if i > 0 {
			out.WriteString("\n")
		}
		out.WriteString(s)
	}
	return out.String()
}
