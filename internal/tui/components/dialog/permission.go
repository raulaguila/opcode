package dialog

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type PermissionAction string

// Permission responses
const (
	PermissionAllow           PermissionAction = "allow"
	PermissionAllowForSession PermissionAction = "allow_session"
	PermissionDeny            PermissionAction = "deny"
)

// PermissionResponseMsg represents the user's response to a permission request
type PermissionResponseMsg struct {
	Permission permission.PermissionRequest
	Action     PermissionAction
}

// PermissionDialogCmp interface for permission dialog component
type PermissionDialogCmp interface {
	util.Model
	layout.Bindings
	SetPermissions(permission permission.PermissionRequest) tea.Cmd
}

type permissionsMapping struct {
	Left         key.Binding
	Right        key.Binding
	EnterSpace   key.Binding
	Allow        key.Binding
	AllowSession key.Binding
	Deny         key.Binding
	Tab          key.Binding
}

var permissionsKeys = permissionsMapping{
	Left: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "switch options"),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→", "switch options"),
	),
	EnterSpace: key.NewBinding(
		key.WithKeys("enter", "space"),
		key.WithHelp("enter/space", "confirm"),
	),
	Allow: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "allow"),
	),
	AllowSession: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "allow for session"),
	),
	Deny: key.NewBinding(
		key.WithKeys("d", "esc"),
		key.WithHelp("d", "deny"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch options"),
	),
}

// permissionDialogCmp is the implementation of PermissionDialog
type permissionDialogCmp struct {
	width           int
	height          int
	permission      permission.PermissionRequest
	windowSize      tea.WindowSizeMsg
	contentViewPort viewport.Model
	fullscreen      bool
	diffMode        string
	activeDiff      string
	selectedOption  int // 0: Allow, 1: Allow for session, 2: Deny
	buttonHits      []permissionButtonHit
	buttonY         int

	diffCache     map[string]string
	markdownCache map[string]string
}

func (p *permissionDialogCmp) Init() tea.Cmd {
	return p.contentViewPort.Init()
}

func (p *permissionDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.windowSize = msg
		cmd := p.SetSize()
		cmds = append(cmds, cmd)
		p.markdownCache = make(map[string]string)
		p.diffCache = make(map[string]string)
	case tea.MouseMsg:
		return updatePermissionDialogCmpMouseMsg(p, msg)
	case tea.KeyPressMsg:
		if result0, result1, handled := updateCaseTeaKeyMsg(p, &cmds, &msg); handled {
			return result0, result1
		}
	}

	return p, tea.Batch(cmds...)
}

func (p *permissionDialogCmp) selectCurrentOption() tea.Cmd {
	var action PermissionAction

	switch p.selectedOption {
	case 0:
		action = PermissionAllow
	case 1:
		action = PermissionAllowForSession
	case 2:
		action = PermissionDeny
	}

	return util.CmdHandler(PermissionResponseMsg{Action: action, Permission: p.permission})
}

type permissionButtonHit struct{ x, y, width int }

func (p *permissionDialogCmp) renderButtons() string {
	t := theme.CurrentTheme()
	labels := []string{config.Text("Permitir (a)", "Allow (a)"), config.Text("Repetir escopo exato (s)", "Remember exact scope (s)"), config.Text("Recusar (Esc/d)", "Deny (Esc/d)")}
	buttons := make([]string, len(labels))
	total := 4
	for i, label := range labels {
		mark := "  "
		style := dialogBody().Padding(0, 1).Background(t.BackgroundSecondary()).Foreground(t.Primary())
		if i == p.selectedOption {
			mark = "› "
			style = style.Background(t.BorderDim()).Foreground(t.Text()).Bold(true)
		}
		buttons[i] = style.Render(mark + label)
		total += lipgloss.Width(buttons[i])
	}
	p.buttonHits = make([]permissionButtonHit, len(buttons))
	if total > max(1, p.width-4) {
		for i, button := range buttons {
			p.buttonHits[i] = permissionButtonHit{2, i, lipgloss.Width(button)}
		}
		return lipgloss.JoinVertical(lipgloss.Left, buttons...)
	}
	offset := max(0, p.width-4-total)
	x := 2 + offset
	for i, button := range buttons {
		p.buttonHits[i] = permissionButtonHit{x, 0, lipgloss.Width(button)}
		x += lipgloss.Width(button) + 2
	}
	return dialogBody().Render(strings.Repeat(" ", offset)) + strings.Join(buttons, "  ")
}

func (p *permissionDialogCmp) renderHeader() string {
	lines := []string{config.Text("Ferramenta: ", "Tool: ") + p.permission.ToolName, config.Text("Escopo: ", "Scope: ") + p.permission.Path}
	return dialogBody().Width(max(1, p.width-4)).Render(strings.Join(lines, "\n"))
}

func (p *permissionDialogCmp) renderBashContent() string {
	t := theme.CurrentTheme()
	baseStyle := dialogBody()

	if pr, ok := p.permission.Params.(tools.BashPermissionsParams); ok {
		content := fmt.Sprintf("```bash\n%s\n```", pr.Command)

		// Use the cache for markdown rendering
		renderedContent := p.GetOrSetMarkdown(p.permission.ID, func() (string, error) {
			r := styles.GetMarkdownRenderer(p.width - 10)
			s, err := r.Render(content)
			return styles.ForceReplaceBackgroundWithLipgloss(s, t.Background()), err
		})

		finalContent := baseStyle.
			Width(p.contentViewPort.Width()).
			Render(renderedContent)
		p.contentViewPort.SetContent(finalContent)
		return p.styleViewport()
	}
	return ""
}

func (p *permissionDialogCmp) renderEditContent() string {
	if pr, ok := p.permission.Params.(tools.EditPermissionsParams); ok {
		return p.diffContent(pr.Diff)
	}
	return ""
}

func (p *permissionDialogCmp) renderPatchContent() string {
	if pr, ok := p.permission.Params.(tools.EditPermissionsParams); ok {
		return p.diffContent(pr.Diff)
	}
	return ""
}

func (p *permissionDialogCmp) renderWriteContent() string {
	if pr, ok := p.permission.Params.(tools.WritePermissionsParams); ok {
		return p.diffContent(pr.Diff)
	}
	return ""
}

func (p *permissionDialogCmp) renderFetchContent() string {
	t := theme.CurrentTheme()
	baseStyle := dialogBody()

	if pr, ok := p.permission.Params.(tools.FetchPermissionsParams); ok {
		content := fmt.Sprintf("```bash\n%s\n```", pr.URL)

		// Use the cache for markdown rendering
		renderedContent := p.GetOrSetMarkdown(p.permission.ID, func() (string, error) {
			r := styles.GetMarkdownRenderer(p.width - 10)
			s, err := r.Render(content)
			return styles.ForceReplaceBackgroundWithLipgloss(s, t.Background()), err
		})

		finalContent := baseStyle.
			Width(p.contentViewPort.Width()).
			Render(renderedContent)
		p.contentViewPort.SetContent(finalContent)
		return p.styleViewport()
	}
	return ""
}

func (p *permissionDialogCmp) renderDefaultContent() string {
	t := theme.CurrentTheme()
	baseStyle := dialogBody()

	content := tools.RedactCredentials(p.permission.Description)
	if p.permission.Params != nil {
		if raw, err := json.MarshalIndent(p.permission.Params, "", "  "); err == nil {
			details := tools.RedactCredentials(string(raw))
			if len(details) > 16384 {
				details = string([]rune(details)[:min(len([]rune(details)), 12000)]) + "\n…"
			}
			content += "\n\n" + config.Text("Parâmetros da ação", "Action parameters") + "\n\n" + "    " + strings.ReplaceAll(details, "\n", "\n    ")
		}
	}

	// Use the cache for markdown rendering
	renderedContent := p.GetOrSetMarkdown(p.permission.ID, func() (string, error) {
		r := styles.GetMarkdownRenderer(p.width - 10)
		s, err := r.Render(content)
		return styles.ForceReplaceBackgroundWithLipgloss(s, t.Background()), err
	})

	finalContent := baseStyle.
		Width(p.contentViewPort.Width()).
		Render(renderedContent)
	p.contentViewPort.SetContent(finalContent)

	if renderedContent == "" {
		return ""
	}

	return p.styleViewport()
}

func (p *permissionDialogCmp) styleViewport() string {
	t := theme.CurrentTheme()
	contentStyle := lipgloss.NewStyle().
		Background(t.Background())

	return contentStyle.Render(p.contentViewPort.View())
}

func (p *permissionDialogCmp) render() string {
	t := theme.CurrentTheme()
	baseStyle := dialogBody()

	title := baseStyle.
		Bold(true).
		Width(p.width - 4).
		Foreground(t.Primary()).
		Render(config.Text("Aprovação necessária", "Permission required"))
	// Render header
	headerContent := p.renderHeader()
	// Render buttons
	buttons := p.renderButtons()

	// Calculate content height dynamically based on window size
	p.contentViewPort.SetHeight(max(1, p.height-lipgloss.Height(headerContent)-lipgloss.Height(buttons)-6-lipgloss.Height(title)))
	p.contentViewPort.SetWidth(max(1, p.width-4))

	// Render content based on tool type
	var contentFinal string
	switch p.permission.ToolName {
	case tools.BashToolName:
		contentFinal = p.renderBashContent()
	case tools.EditToolName:
		contentFinal = p.renderEditContent()
	case tools.PatchToolName:
		contentFinal = p.renderPatchContent()
	case tools.WriteToolName:
		contentFinal = p.renderWriteContent()
	case tools.FetchToolName:
		contentFinal = p.renderFetchContent()
	default:
		contentFinal = p.renderDefaultContent()
	}

	p.buttonY = 2 + lipgloss.Height(title) + 1 + lipgloss.Height(headerContent) + lipgloss.Height(contentFinal)
	content := lipgloss.JoinVertical(
		lipgloss.Top,
		title,
		baseStyle.Render(strings.Repeat(" ", lipgloss.Width(title))),
		headerContent,
		contentFinal,
		buttons,
		styles.PanelDivider(max(1, p.width-4)),
		p.permissionHintView(max(1, p.width-4)),
		baseStyle.Render(strings.Repeat(" ", max(0, p.width-4))),
	)

	return baseStyle.
		Padding(1, 0, 0, 1).
		Border(lipgloss.NormalBorder()).
		BorderBackground(t.BackgroundSecondary()).
		BorderForeground(t.TextMuted()).
		Width(p.width).
		Height(p.height).
		Render(
			content,
		)
}

func (p *permissionDialogCmp) View() string {
	return p.render()
}

func (p *permissionDialogCmp) BindingKeys() []key.Binding {
	return append(layout.KeyMapToSlice(permissionsKeys), key.NewBinding(key.WithKeys("t"), key.WithHelp("t", config.Text("alternar diff", "toggle diff"))), key.NewBinding(key.WithKeys("f"), key.WithHelp("f", config.Text("tela cheia", "full screen"))), key.NewBinding(key.WithKeys("[", "]"), key.WithHelp("[ ]", config.Text("arquivo anterior/próximo", "previous/next file"))))
}

func (p *permissionDialogCmp) SetSize() tea.Cmd {
	if p.permission.ID == "" {
		return nil
	}
	w, h := p.windowSize.Width, p.windowSize.Height
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 30
	}
	p.width = max(10, min(w-4, 120))
	p.height = max(10, h-4)
	if p.fullscreen {
		p.width = max(10, w-4)
		p.height = max(10, h-4)
	}
	return nil
}

func (p *permissionDialogCmp) SetPermissions(permission permission.PermissionRequest) tea.Cmd {
	p.permission = permission
	p.selectedOption = 2
	p.buttonHits = nil
	p.contentViewPort.GotoTop()
	p.diffCache = map[string]string{}
	p.markdownCache = map[string]string{}
	return p.SetSize()
}

// Helper to get or set cached diff content
func (c *permissionDialogCmp) GetOrSetDiff(key string, generator func() (string, error)) string {
	if cached, ok := c.diffCache[key]; ok {
		return cached
	}

	content, err := generator()
	if err != nil {
		return fmt.Sprintf("Error formatting diff: %v", err)
	}

	c.diffCache[key] = content

	return content
}

// Helper to get or set cached markdown content
func (c *permissionDialogCmp) GetOrSetMarkdown(key string, generator func() (string, error)) string {
	if cached, ok := c.markdownCache[key]; ok {
		return cached
	}

	content, err := generator()
	if err != nil {
		return fmt.Sprintf("Error rendering markdown: %v", err)
	}

	c.markdownCache[key] = content

	return content
}

func NewPermissionDialogCmp() PermissionDialogCmp {
	// Create viewport for content
	contentViewport := viewport.New()

	return &permissionDialogCmp{
		contentViewPort: contentViewport,
		diffMode:        "auto",
		selectedOption:  2, // A new request never inherits consent.
		diffCache:       make(map[string]string),
		markdownCache:   make(map[string]string),
	}
}

func (p *permissionDialogCmp) permissionHint() string {
	switch p.permission.ToolName {
	case tools.EditToolName, tools.PatchToolName, tools.WriteToolName:
		return config.Text("T diff · F tela cheia · [ ] arquivo", "T diff · F full screen · [ ] file")
	default:
		return config.Text("F tela cheia · escopo exato nesta sessão", "F full screen · exact scope for this session")
	}
}

func (p *permissionDialogCmp) permissionHintView(width int) string {
	switch p.permission.ToolName {
	case tools.EditToolName, tools.PatchToolName, tools.WriteToolName:
		return dialogHint(p.permissionHint(), width)
	default:
		// The exact-scope statement is consent information, not a muted legend.
		return dialogBody().Width(width).Render(p.permissionHint())
	}
}

func updatePermissionDialogCmpMouseMsg(p *permissionDialogCmp, msg tea.MouseMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd) {
	if leftClick(msg) {
		for i, hit := range p.buttonHits {
			if msg.Mouse().Y == p.buttonY+hit.y && msg.Mouse().X >= hit.x && msg.Mouse().X < hit.x+hit.width {
				p.selectedOption = i
				return p, p.selectCurrentOption()
			}
		}
	}
	if msg.Mouse().Button == tea.MouseWheelUp {
		p.contentViewPort.ScrollUp(3)
	}
	if msg.Mouse().Button == tea.MouseWheelDown {
		p.contentViewPort.ScrollDown(3)
	}
	return p, nil

}

func updateCaseTeaKeyMsg(p *permissionDialogCmp, cmds *[]tea.Cmd, msg *tea.KeyPressMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	// Native key text can contain "a" even for Alt+A. Consent shortcuts
	// must match the complete keystroke, including its modifiers.
	bindingKey := *msg
	bindingKey.Text = ""
	if msg.String() == "f" {
		p.fullscreen = !p.fullscreen
		p.SetSize()
		p.diffCache = map[string]string{}
		return p, nil, true
	}
	if msg.String() == "t" {
		switch p.diffMode {
		case "auto":
			p.diffMode = "unified"
		case "unified":
			p.diffMode = "split"
		default:
			p.diffMode = "auto"
		}
		p.diffCache = map[string]string{}
		return p, nil, true
	}
	if msg.String() == "]" || msg.String() == "[" {
		delta := 1
		if msg.String() == "[" {
			delta = -1
		}
		p.nextFile(delta)
		return p, nil, true
	}
	switch {
	case key.Matches(bindingKey, permissionsKeys.Right) || key.Matches(bindingKey, permissionsKeys.Tab):
		p.selectedOption = (p.selectedOption + 1) % 3
		return p, nil, true
	case key.Matches(bindingKey, permissionsKeys.Left):
		p.selectedOption = (p.selectedOption + 2) % 3
	case key.Matches(bindingKey, permissionsKeys.EnterSpace):
		return p, p.selectCurrentOption(), true
	case key.Matches(bindingKey, permissionsKeys.Allow):
		return p, util.CmdHandler(PermissionResponseMsg{Action: PermissionAllow, Permission: p.permission}), true
	case key.Matches(bindingKey, permissionsKeys.AllowSession):
		return p, util.CmdHandler(PermissionResponseMsg{Action: PermissionAllowForSession, Permission: p.permission}), true
	case key.Matches(bindingKey, permissionsKeys.Deny):
		return p, util.CmdHandler(PermissionResponseMsg{Action: PermissionDeny, Permission: p.permission}), true
	default:
		// Pass other keys to viewport
		viewPort, cmd := p.contentViewPort.Update((*msg))
		p.contentViewPort = viewPort
		(*cmds) = append((*cmds), cmd)
	}
	return extractedResult0, extractedResult1, false
}
