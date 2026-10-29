package dialog

import (
	"fmt"
	"github.com/raulaguila/opcode/internal/config"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	tea.Model
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
func (s *sessionDialogCmp) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
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
	case tea.MouseMsg:
		if s.editing {
			return s, nil
		}
		if msg.Button == tea.MouseButtonWheelUp {
			s.move(-1)
		}
		if msg.Button == tea.MouseButtonWheelDown {
			s.move(1)
		}
		row := msg.Y - 4
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && row >= 0 && row < s.rows() && s.offset+row < len(s.filtered) {
			s.selectedIdx = s.offset + row
			return s, util.CmdHandler(SessionSelectedMsg{s.filtered[s.selectedIdx]})
		}
	case tea.KeyMsg:
		if s.editing {
			switch msg.Type {
			case tea.KeyEsc:
				s.editing = false
			case tea.KeyEnter:
				title := strings.TrimSpace(s.rename)
				if title != "" && len(s.filtered) > 0 {
					s.editing = false
					return s, util.CmdHandler(SessionActionMsg{Action: "rename", Session: s.filtered[s.selectedIdx], Title: title})
				}
			case tea.KeyCtrlU:
				s.rename = ""
			case tea.KeyBackspace:
				_, n := utf8.DecodeLastRuneInString(s.rename)
				if n > 0 {
					s.rename = s.rename[:len(s.rename)-n]
				}
			case tea.KeyRunes:
				for _, r := range msg.Runes {
					if !unicode.IsControl(r) && len(s.rename) < 180 {
						s.rename += string(r)
					}
				}
			}
			return s, nil
		}
		switch msg.String() {
		case "esc":
			return s, util.CmdHandler(CloseSessionDialogMsg{})
		case "up":
			s.move(-1)
		case "down":
			s.move(1)
		case "enter":
			if len(s.filtered) > 0 {
				return s, util.CmdHandler(SessionSelectedMsg{s.filtered[s.selectedIdx]})
			}
		case "ctrl+r":
			if len(s.filtered) > 0 {
				s.rename = s.filtered[s.selectedIdx].Title
				s.editing = true
			}
		case "ctrl+a":
			if len(s.filtered) > 0 {
				return s, util.CmdHandler(SessionActionMsg{Action: "archive", Session: s.filtered[s.selectedIdx]})
			}
		case "ctrl+e":
			if len(s.filtered) > 0 {
				return s, util.CmdHandler(SessionActionMsg{Action: "export", Session: s.filtered[s.selectedIdx]})
			}
		case "ctrl+h":
			s.showArchived = !s.showArchived
			s.filter()
		default:
			if msg.Type == tea.KeyBackspace {
				_, n := utf8.DecodeLastRuneInString(s.query)
				if n > 0 {
					s.query = s.query[:len(s.query)-n]
					s.filter()
				}
			}
			if msg.Type == tea.KeyRunes {
				for _, r := range msg.Runes {
					if !unicode.IsControl(r) && len(s.query) < 128 {
						s.query += string(r)
					}
				}
				s.filter()
			}
		}
	}
	return s, nil
}
func (s *sessionDialogCmp) rows() int {
	if s.height == 0 {
		return 10
	}
	reserve := 10
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
	base := styles.BaseStyle().Width(width)
	line := func(text string) string { return base.Render(ansi.Truncate(text, width, "…")) }
	rows := []string{base.Foreground(t.Primary()).Bold(true).Render(config.Text("Conversas", "Conversations")), line(config.Text("Buscar: ", "Search: ") + s.query)}
	if len(s.filtered) == 0 {
		rows = append(rows, line(config.Text("Nenhuma conversa encontrada", "No conversations found")))
	}
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
		row := base
		if i == s.selectedIdx {
			row = row.Background(t.Primary()).Foreground(t.Background())
		}
		rows = append(rows, row.Render(ansi.Truncate(label, width, "…")))
	}
	if len(s.filtered) > 0 && !s.editing && (s.height == 0 || s.height >= 18) {
		preview := config.Text("Carregando prévia…", "Loading preview…")
		if s.previewID == s.filtered[s.selectedIdx].ID {
			preview = s.preview
		}
		rows = append(rows, line(""), line(preview))
	}
	if s.editing {
		rows = append(rows, line(config.Text("Novo título: ", "New title: ")+s.rename), line(config.Text("Enter salva · Esc volta", "Enter save · Esc back")))
	} else {
		rows = append(rows, line(fmt.Sprintf(config.Text("%d conversas · arquivadas: %t", "%d conversations · archived: %t"), len(s.filtered), s.showArchived)), line(config.Text("↑↓ navegar · Enter abrir · Ctrl+R renomear", "↑↓ navigate · Enter open · Ctrl+R rename")), line(config.Text("Ctrl+A arquivar/restaurar · Ctrl+E exportar", "Ctrl+A archive/restore · Ctrl+E export")), line(config.Text("Ctrl+H mostrar arquivadas · Esc fechar", "Ctrl+H show archived · Esc close")))
	}
	return styles.BaseStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(t.TextMuted()).Render(strings.Join(rows, "\n"))
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
