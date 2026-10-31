package dialog

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/raulaguila/opcode/internal/config"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type SessionSelectedMsg struct{ Session session.Session }
type CloseSessionDialogMsg struct{}
type SessionPreviewRequestMsg struct{ ID string }
type SessionPreviewLoadedMsg struct{ ID, Text string }
type SessionActionMsg struct {
	Action  string
	Session session.Session
	Title   string
}

type SessionDialog interface {
	util.Model
	layout.Bindings
	SetSessions([]session.Session)
	SetSelectedSession(string)
	SetArchived(map[string]bool)
}
type sessionDialogCmp struct {
	sessions, filtered                   []session.Session
	selectedIdx, width, height, offset   int
	selectedSessionID, query, rename     string
	previewID, preview, requestedPreview string
	editing, showArchived                bool
	archived                             map[string]bool
}

func (s *sessionDialogCmp) Init() tea.Cmd { return s.requestPreview() }
func (s *sessionDialogCmp) Update(msg tea.Msg) (model util.Model, cmd tea.Cmd) {
	defer func() {
		if preview := s.requestPreviewIfIdle(cmd); preview != nil {
			cmd = tea.Batch(cmd, preview)
		}
	}()
	switch msg := msg.(type) {
	case SessionPreviewLoadedMsg:
		if msg.ID == s.requestedPreview {
			s.previewID, s.preview = msg.ID, msg.Text
		}

	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		s.move(0)
	case tea.MouseMsg:
		if result0, result1, handled := updateCaseTeaMouseMsg(s, msg); handled {
			return result0, result1
		}
	case tea.KeyPressMsg:
		if result0, result1, handled := updateSessionDialogCmpKeyMsg(s, &msg); handled {
			return result0, result1
		}
	case tea.PasteMsg:
		key := tea.KeyPressMsg{Text: msg.Content}
		if s.editing {
			updateSessionDialogCmpKeyMsgSEditingCaseTeaKeyRunes(s, &key)
		} else {
			updateSessionDialogCmpKeyMsgDefault(s, &key)
		}
	}
	return s, nil
}
func (s *sessionDialogCmp) rows() int {
	if s.height == 0 {
		return 10
	}
	reserve := 10
	if s.height >= 16 {
		reserve++
	}
	if s.height >= 18 {
		reserve += 2
	}
	return max(1, min(10, s.height-reserve))
}
func (s *sessionDialogCmp) move(delta int) {
	if len(s.filtered) == 0 {
		return
	}
	s.selectedIdx = (s.selectedIdx + delta + len(s.filtered)) % len(s.filtered)
	if s.selectedIdx < s.offset {
		s.offset = s.selectedIdx
	}
	if s.selectedIdx >= s.offset+s.rows() {
		s.offset = s.selectedIdx - s.rows() + 1
	}
}
func (s *sessionDialogCmp) filter() {
	selected := s.selectedSessionID
	if len(s.filtered) > s.selectedIdx && s.selectedIdx >= 0 {
		selected = s.filtered[s.selectedIdx].ID
	}
	s.filtered = nil
	for _, item := range s.sessions {
		if (!s.archived[item.ID] || s.showArchived) && strings.Contains(strings.ToLower(item.Title+" "+item.ID), strings.ToLower(s.query)) {
			s.filtered = append(s.filtered, item)
		}
	}
	s.selectedIdx, s.offset = 0, 0
	for i, item := range s.filtered {
		if item.ID == selected {
			s.selectedIdx = i
			break
		}
	}
	if s.selectedIdx >= s.rows() {
		s.offset = s.selectedIdx - s.rows() + 1
	}
}
func (s *sessionDialogCmp) View() string {
	width := min(74, max(1, s.width-8))
	if s.width == 0 {
		width = 60
	}
	t := theme.CurrentTheme()
	base := dialogBody().Width(width)
	line := func(text string) string { return base.Render(ansi.Truncate(text, width, "…")) }
	rows := []string{base.Foreground(t.Primary()).Bold(true).Render(config.Text("Conversas", "Conversations")), line(config.Text("Buscar: ", "Search: ") + s.query)}
	if len(s.filtered) == 0 {
		rows = append(rows, line(config.Text("Nenhuma conversa encontrada", "No conversations found")))
	}
	viewSOffset(s, width, t, &base, &rows)
	if len(s.filtered) > 0 && !s.editing && (s.height == 0 || s.height >= 18) {
		preview := config.Text("Carregando prévia…", "Loading preview…")
		if s.previewID == s.filtered[s.selectedIdx].ID {
			preview = s.preview
		}
		rows = append(rows, line(""), line(preview))
	}
	if s.editing {
		rows = append(rows, line(config.Text("Novo título: ", "New title: ")+s.rename))
		rows = dialogFooter(rows, width, s.height, line(config.Text("Enter salva · Esc volta", "Enter save · Esc back")))
	} else {
		rows = append(rows, line(fmt.Sprintf(config.Text("%d conversas", "%d conversations"), len(s.filtered))))
		rows = dialogFooter(rows, width, s.height, line(config.Text("↑↓ navegar · Enter abrir · Ctrl+R renomear", "↑↓ navigate · Enter open · Ctrl+R rename")), line(config.Text("Ctrl+A arquivar/restaurar · Ctrl+E exportar", "Ctrl+A archive/restore · Ctrl+E export")), line(config.Text("Ctrl+H mostrar arquivadas · Esc fechar", "Ctrl+H show archived · Esc close")))
	}
	return styles.Panel().Padding(1, 2).Border(lipgloss.NormalBorder()).BorderForeground(t.TextMuted()).Render(strings.Join(rows, "\n"))
}
func (s *sessionDialogCmp) BindingKeys() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "conversas"))}
}
func (s *sessionDialogCmp) SetSessions(items []session.Session) {
	s.sessions = slices.Clone(items)
	slices.SortStableFunc(s.sessions, func(a, b session.Session) int {
		if a.UpdatedAt > b.UpdatedAt {
			return -1
		}
		if a.UpdatedAt < b.UpdatedAt {
			return 1
		}
		return 0
	})
	s.previewID, s.preview, s.requestedPreview = "", "", ""
	s.filter()
}
func (s *sessionDialogCmp) SetSelectedSession(id string)         { s.selectedSessionID = id; s.filter() }
func (s *sessionDialogCmp) SetArchived(archived map[string]bool) { s.archived = archived; s.filter() }
func NewSessionDialogCmp() SessionDialog                         { return &sessionDialogCmp{archived: map[string]bool{}} }

func (s *sessionDialogCmp) requestPreview() tea.Cmd {
	if len(s.filtered) == 0 {
		return nil
	}
	id := s.filtered[s.selectedIdx].ID
	if s.requestedPreview == id {
		return nil
	}
	s.requestedPreview = id
	return util.CmdHandler(SessionPreviewRequestMsg{ID: id})
}
func sessionRecency(updated int64, now time.Time) string {
	if updated <= 0 {
		return config.Text("sem data", "undated")
	}
	elapsed := now.Sub(time.Unix(updated, 0))
	switch {
	case elapsed < time.Minute:
		return config.Text("agora", "now")
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	case elapsed < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
	default:
		return time.Unix(updated, 0).Format("2006-01-02")
	}
}

func (s *sessionDialogCmp) requestPreviewIfIdle(cmd tea.Cmd) tea.Cmd {
	if cmd != nil || s.editing {
		return nil
	}
	return s.requestPreview()
}

func updateSessionDialogCmpKeyMsg(s *sessionDialogCmp, msg *tea.KeyPressMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if result0, result1, result2, handled := updateSessionDialogCmpKeyMsgSEditing(s, msg); handled {
		return result0, result1, result2
	}
	switch msg.String() {
	case "esc":
		return s, util.CmdHandler(CloseSessionDialogMsg{}), true
	case "up":
		s.move(-1)
	case "down":
		s.move(1)
	case "enter":
		if len(s.filtered) > 0 {
			return s, util.CmdHandler(SessionSelectedMsg{s.filtered[s.selectedIdx]}), true
		}
	case "ctrl+r":
		if len(s.filtered) > 0 {
			s.rename = s.filtered[s.selectedIdx].Title
			s.editing = true
		}
	case "ctrl+a":
		if len(s.filtered) > 0 {
			return s, util.CmdHandler(SessionActionMsg{Action: "archive", Session: s.filtered[s.selectedIdx]}), true
		}
	case "ctrl+e":
		if len(s.filtered) > 0 {
			return s, util.CmdHandler(SessionActionMsg{Action: "export", Session: s.filtered[s.selectedIdx]}), true
		}
	case "ctrl+h":
		s.showArchived = !s.showArchived
		s.filter()
	default:
		updateSessionDialogCmpKeyMsgDefault(s, msg)
	}
	return extractedResult0, extractedResult1, false
}

func updateSessionDialogCmpKeyMsgDefault(s *sessionDialogCmp, msg *tea.KeyPressMsg) {
	if msg.Code == tea.KeyBackspace {
		_, n := utf8.DecodeLastRuneInString(s.query)
		if n > 0 {
			s.query = s.query[:len(s.query)-n]
			s.filter()
		}
	}
	if msg.Text != "" {
		for _, r := range msg.Text {
			if !unicode.IsControl(r) && len(s.query) < 128 {
				s.query += string(r)
			}
		}
		s.filter()
	}
}

func updateSessionDialogCmpKeyMsgSEditing(s *sessionDialogCmp, msg *tea.KeyPressMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if s.editing {
		switch msg.String() {
		case "esc":
			s.editing = false
		case "enter":
			title := strings.TrimSpace(s.rename)
			if title != "" && len(s.filtered) > 0 {
				s.editing = false
				return s, util.CmdHandler(SessionActionMsg{Action: "rename", Session: s.filtered[s.selectedIdx], Title: title}), true, true
			}
		case "ctrl+u":
			s.rename = ""
		case "backspace":
			_, n := utf8.DecodeLastRuneInString(s.rename)
			if n > 0 {
				s.rename = s.rename[:len(s.rename)-n]
			}
		default:
			if msg.Text != "" {
				updateSessionDialogCmpKeyMsgSEditingCaseTeaKeyRunes(s, msg)
			}
		}
		return s, nil, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func viewSOffset(s *sessionDialogCmp, width int, t theme.Theme, base *lipgloss.Style, rows *[]string) {
	for i := s.offset; i < min(len(s.filtered), s.offset+s.rows()); i++ {
		item := s.filtered[i]
		label := ansi.Strip(item.Title)
		if i == s.selectedIdx {
			label = "› " + label
		} else {
			label = "  " + label
		}
		if item.ID == s.selectedSessionID {
			label = "● " + label
		}
		if s.archived[item.ID] {
			label += config.Text(" · arquivada", " · archived")
		}
		if width >= 48 {
			age := sessionRecency(item.UpdatedAt, time.Now())
			label = ansi.Truncate(label, max(1, width-ansi.StringWidth(age)-2), "…")
			label += strings.Repeat(" ", max(2, width-ansi.StringWidth(label)-ansi.StringWidth(age))) + age
		}
		row := (*base)
		if i == s.selectedIdx {
			row = row.Background(t.BorderDim()).Foreground(t.Text())
		}
		(*rows) = append((*rows), row.Render(ansi.Truncate(label, width, "…")))
	}
}

func updateSessionDialogCmpKeyMsgSEditingCaseTeaKeyRunes(s *sessionDialogCmp, msg *tea.KeyPressMsg) {
	for _, r := range msg.Text {
		if !unicode.IsControl(r) && len(s.rename) < 180 {
			s.rename += string(r)
		}
	}
}

func updateCaseTeaMouseMsg(s *sessionDialogCmp, msg tea.MouseMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if s.editing {
		return s, nil, true
	}
	if msg.Mouse().Button == tea.MouseWheelUp {
		s.move(-1)
	}
	if msg.Mouse().Button == tea.MouseWheelDown {
		s.move(1)
	}
	row := msg.Mouse().Y - 4
	if leftClick(msg) && row >= 0 && row < s.rows() && s.offset+row < len(s.filtered) {
		s.selectedIdx = s.offset + row
		return s, s.requestPreview(), true
	}
	return extractedResult0, extractedResult1, false
}
