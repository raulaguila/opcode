package chat

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type editorCmp struct {
	width                 int
	height                int
	app                   *app.App
	session               session.Session
	textarea              textarea.Model
	attachments           []message.Attachment
	deleteMode            bool
	submitting            bool
	slash                 slashState
	history               []string
	historyIndex          int
	browsingHistory       bool
	historyDraft          string
	drafts                map[string]editorDraft
	draftInitError        error
	draftRoot             string
	draftPath             string
	preparedDraft         *preparedSessionDraft
	submissionSession     string
	submissionText        string
	submissionUnassigned  bool
	submissionAttachments []message.Attachment
}

type EditorKeyMaps struct {
	Send       key.Binding
	OpenEditor key.Binding
}

type bluredEditorKeyMaps struct {
	Send       key.Binding
	Focus      key.Binding
	OpenEditor key.Binding
}
type DeleteAttachmentKeyMaps struct {
	AttachmentDeleteMode key.Binding
	Escape               key.Binding
	DeleteAllAttachments key.Binding
}

var editorMaps = EditorKeyMaps{
	Send: key.NewBinding(
		key.WithKeys("enter", "ctrl+s"),
		key.WithHelp("enter", "send message"),
	),
	OpenEditor: key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl+e", "open editor"),
	),
}

var DeleteKeyMaps = DeleteAttachmentKeyMaps{
	AttachmentDeleteMode: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r+{i}", "delete attachment at index i"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel delete mode"),
	),
	DeleteAllAttachments: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("ctrl+r+r", "delete all attchments"),
	),
}

const (
	maxAttachments = 5
)

type editorResultMsg struct{ Text string }

func (m *editorCmp) openEditor() tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}
	file, err := os.CreateTemp("", "msg_*.md")
	if err != nil {
		return util.ReportError(err)
	}
	path := file.Name()
	if _, err := file.WriteString(m.textarea.Value()); err != nil {
		file.Close()
		os.Remove(path)
		return util.ReportError(err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return util.ReportError(err)
	}
	command := exec.Command(editor, path)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return tea.ExecProcess(command, func(editorErr error) tea.Msg {
		defer os.Remove(path)
		if editorErr != nil {
			return util.InfoMsg{Type: util.InfoTypeError, Msg: editorErr.Error()}
		}
		source, err := os.Open(path)
		if err != nil {
			return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
		}
		defer source.Close()
		raw, err := io.ReadAll(io.LimitReader(source, 64001))
		if err != nil {
			return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
		}
		if len(raw) > 64000 {
			return util.InfoMsg{Type: util.InfoTypeError, Msg: "Editor message exceeds 64 KB; original draft was preserved"}
		}
		if len(raw) == 0 {
			return util.InfoMsg{Type: util.InfoTypeWarn, Msg: "Message is empty; original draft was preserved"}
		}
		return editorResultMsg{Text: string(raw)}
	})
}

func (m *editorCmp) Init() tea.Cmd {
	if m.draftInitError != nil {
		return tea.Batch(textarea.Blink, util.ReportError(m.draftInitError))
	}
	return textarea.Blink
}

func (m *editorCmp) send() tea.Cmd {
	if m.submitting {
		return nil
	}

	value := m.textarea.Value()
	attachments := cloneAttachments(m.attachments)
	if value == "" {
		return nil
	}
	m.submitting = true
	m.submissionSession = m.session.ID
	m.submissionText = value
	m.submissionUnassigned = m.session.ID == ""
	m.submissionAttachments = cloneAttachments(attachments)
	return tea.Batch(
		util.CmdHandler(SendMsg{
			Text:        value,
			Attachments: attachments,
		}),
	)
}

func (m *editorCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.(type) {
	case tea.KeyMsg, dialog.AttachmentAddedMsg, dialog.CompletionSelectedMsg, editorResultMsg, RestoreDraftMsg, SubmissionResultMsg:
		m.preparedDraft = nil
	}
	switch msg := msg.(type) {
	case editorResultMsg:
		m.textarea.SetValue(msg.Text)
		m.textarea.CursorEnd()
		return m, m.send()
	case SaveDraftMsg:
		if err := m.PersistDraft(); err != nil {
			return m, util.ReportError(err)
		}
		return m, nil
	case RestoreDraftMsg:
		if m.textarea.Value() != "" || len(m.attachments) > 0 {
			return m, util.ReportWarn("O rascunho atual foi preservado; limpe-o antes de editar a instrução")
		}
		m.textarea.SetValue(msg.Text)
		m.textarea.CursorEnd()
		m.attachments = cloneAttachments(msg.Attachments)
		return m, nil
	case SubmissionResultMsg:
		sameSession := m.submissionText == "" || m.session.ID == m.submissionSession
		m.submitting = false
		if msg.Error == nil && msg.Text != "" && sameSession {
			m.history = append(m.history, msg.Text)
			if len(m.history) > 200 {
				m.history = m.history[len(m.history)-200:]
			}
			m.browsingHistory = false
		}
		if msg.Error == nil && sameSession && m.textarea.Value() == msg.Text && (m.submissionText == "" || sameAttachments(m.attachments, m.submissionAttachments)) {
			m.textarea.Reset()
			m.attachments = nil
		}
		if msg.Error == nil {
			if !sameSession && m.submissionText != "" {
				if err := m.persistDraftValue(m.submissionSession, editorDraft{}); err != nil {
					return m, util.ReportError(err)
				}
			}
			if m.submissionUnassigned {
				if err := m.persistDraftValue("", editorDraft{}); err != nil {
					return m, util.ReportError(err)
				}
			}
			m.submissionText = ""
			m.submissionUnassigned = false
			m.submissionAttachments = nil
			if err := m.PersistDraft(); err != nil {
				return m, util.ReportError(err)
			}
		}
		return m, nil
	case dialog.ThemeChangedMsg:
		m.textarea = CreateTextArea(&m.textarea)
	case dialog.CompletionSelectedMsg:
		existingValue := m.textarea.Value()
		modifiedValue := strings.Replace(existingValue, msg.SearchString, msg.CompletionValue, 1)

		m.textarea.SetValue(modifiedValue)
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			if err := m.PrepareSessionDraft(msg.ID); err != nil {
				return m, util.ReportError(err)
			}
			prepared := m.preparedDraft
			m.preparedDraft = nil
			m.session = msg
			if prepared != nil && prepared.keepCurrent {
				m.submissionSession = msg.ID
				return m, nil
			}
			if prepared != nil {
				m.applyDraft(prepared.draft)
				m.history = prepared.history
			}
		}
		return m, nil
	case dialog.AttachmentAddedMsg:
		if len(m.attachments) >= maxAttachments {
			logging.ErrorPersist(fmt.Sprintf("cannot add more than %d images", maxAttachments))
			return m, cmd
		}
		m.attachments = append(m.attachments, msg.Attachment)
	case SessionClearedMsg:
		if m.session.ID != "" {
			if err := m.PrepareSessionDraft(""); err != nil {
				return m, util.ReportError(err)
			}
		} else {
			if err := m.PersistDraft(); err != nil {
				return m, util.ReportError(err)
			}
		}
		prepared := m.preparedDraft
		m.preparedDraft = nil
		m.session = session.Session{}
		if prepared != nil {
			m.applyDraft(prepared.draft)
			m.history = prepared.history
		} else {
			m.textarea.Reset()
			m.attachments = nil
			m.history = nil
		}
		m.browsingHistory = false
		return m, nil
	case tea.KeyMsg:
		if cmd, handled := m.slashKey(msg); handled {
			return m, cmd
		}
		if m.promptHistoryKey(msg) {
			return m, nil
		}
		if msg.String() == "alt+enter" || msg.String() == "shift+enter" || msg.Type == tea.KeyCtrlJ {
			m.textarea.InsertString("\n")
			return m, nil
		}
		if key.Matches(msg, DeleteKeyMaps.AttachmentDeleteMode) {
			m.deleteMode = true
			return m, nil
		}
		if key.Matches(msg, DeleteKeyMaps.DeleteAllAttachments) && m.deleteMode {
			m.deleteMode = false
			m.attachments = nil
			return m, nil
		}
		if m.deleteMode && len(msg.Runes) > 0 && unicode.IsDigit(msg.Runes[0]) {
			num := int(msg.Runes[0] - '0')
			m.deleteMode = false
			if num < 10 && len(m.attachments) > num {
				if num == 0 {
					m.attachments = m.attachments[num+1:]
				} else {
					m.attachments = slices.Delete(m.attachments, num, num+1)
				}
				return m, nil
			}
		}
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			return m, nil
		}
		if key.Matches(msg, editorMaps.OpenEditor) {
			if m.app.CoderAgent != nil && m.app.CoderAgent.IsSessionBusy(m.session.ID) {
				return m, util.ReportWarn("Agent is working, please wait...")
			}
			return m, m.openEditor()
		}
		if key.Matches(msg, DeleteKeyMaps.Escape) {
			m.deleteMode = false
			return m, nil
		}
		// Hanlde Enter key
		if m.textarea.Focused() && key.Matches(msg, editorMaps.Send) {
			value := m.textarea.Value()
			if len(value) > 0 && value[len(value)-1] == '\\' {
				// If the last character is a backslash, remove it and add a newline
				m.textarea.SetValue(value[:len(value)-1] + "\n")
				return m, nil
			} else {
				// Otherwise, send the message
				return m, m.send()
			}
		}

	}
	previous := m.textarea.Value()
	m.textarea, cmd = m.textarea.Update(msg)
	if m.textarea.Value() != previous {
		m.slash = slashState{}
	}
	return m, cmd
}

func (m *editorCmp) View() string {
	t := theme.CurrentTheme()

	// Style the prompt with theme colors
	style := lipgloss.NewStyle().
		Padding(0, 0, 0, 1).
		Bold(true).
		Foreground(t.Primary())

	if len(m.attachments) == 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, style.Render(">"), m.textarea.View())
	}
	m.textarea.SetHeight(m.height - 1)
	return lipgloss.JoinVertical(lipgloss.Top,
		m.attachmentsContent(),
		lipgloss.JoinHorizontal(lipgloss.Top, style.Render(">"),
			m.textarea.View()),
	)
}

func (m *editorCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	m.textarea.SetWidth(width - 3) // account for the prompt and padding right
	m.textarea.SetHeight(height)
	m.textarea.SetWidth(width)
	return nil
}

func (m *editorCmp) GetSize() (int, int) {
	return m.textarea.Width(), m.textarea.Height()
}

func (m *editorCmp) attachmentsContent() string {
	var styledAttachments []string
	t := theme.CurrentTheme()
	attachmentStyles := styles.BaseStyle().
		MarginLeft(1).
		Background(t.TextMuted()).
		Foreground(t.Text())
	for i, attachment := range m.attachments {
		var filename string
		if len(attachment.FileName) > 10 {
			filename = fmt.Sprintf(" %s %s...", styles.DocumentIcon, attachment.FileName[0:7])
		} else {
			filename = fmt.Sprintf(" %s %s", styles.DocumentIcon, attachment.FileName)
		}
		if m.deleteMode {
			filename = fmt.Sprintf("%d%s", i, filename)
		}
		styledAttachments = append(styledAttachments, attachmentStyles.Render(filename))
	}
	content := lipgloss.JoinHorizontal(lipgloss.Left, styledAttachments...)
	return content
}

func (m *editorCmp) BindingKeys() []key.Binding {
	bindings := []key.Binding{}
	bindings = append(bindings, layout.KeyMapToSlice(editorMaps)...)
	bindings = append(bindings, layout.KeyMapToSlice(DeleteKeyMaps)...)
	return bindings
}

func CreateTextArea(existing *textarea.Model) textarea.Model {
	t := theme.CurrentTheme()
	bgColor := t.Background()
	textColor := t.Text()
	textMutedColor := t.TextMuted()

	ta := textarea.New()
	ta.BlurredStyle.Base = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.BlurredStyle.CursorLine = styles.BaseStyle().Background(bgColor)
	ta.BlurredStyle.Placeholder = styles.BaseStyle().Background(bgColor).Foreground(textMutedColor)
	ta.BlurredStyle.Text = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.Base = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.CursorLine = styles.BaseStyle().Background(bgColor)
	ta.FocusedStyle.Placeholder = styles.BaseStyle().Background(bgColor).Foreground(textMutedColor)
	ta.FocusedStyle.Text = styles.BaseStyle().Background(bgColor).Foreground(textColor)

	ta.Prompt = " "
	ta.ShowLineNumbers = false
	ta.CharLimit = -1

	if existing != nil {
		ta.SetValue(existing.Value())
		ta.SetWidth(existing.Width())
		ta.SetHeight(existing.Height())
	}

	ta.Focus()
	return ta
}

func NewEditorCmp(app *app.App) tea.Model {
	root, path := draftLocation()
	return newEditorWithDraftStorage(app, root, path)
}
func newEditorWithDraftStorage(app *app.App, root, path string) *editorCmp {
	editor := &editorCmp{app: app, textarea: CreateTextArea(nil), drafts: map[string]editorDraft{}, draftRoot: root, draftPath: path}
	editor.draftInitError = editor.restoreSessionDraft()
	return editor
}
