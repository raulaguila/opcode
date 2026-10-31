package tui

import (
	"fmt"
	"github.com/raulaguila/opcode/internal/tui/util"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// This is routed component acceptance, not a physical-terminal or provider test.
// Do not run subtests in parallel: config, palette and terminal color profile are
// deliberately process-global production seams.
func TestRoutedModalRegressionMatrix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	if config.Get() == nil {
		root := t.TempDir()
		file := filepath.Join(root, "config.json")
		require.NoError(t, os.WriteFile(file, []byte(`{"providers":{"openai":{"apiKey":"matrix-placeholder"}}}`), 0600))
		viper.SetConfigFile(file)
		_, err := config.Load(root, false)
		require.NoError(t, err)
	}
	cfg := config.Get()
	old := *cfg
	oldConfig := viper.ConfigFileUsed()
	oldProfile := theme.ColorProfile()
	oldDark := theme.HasDarkBackground()
	oldTheme := themeNameForMatrix()
	t.Cleanup(func() {
		*cfg = old
		viper.SetConfigFile(oldConfig)
		theme.SetColorProfile(oldProfile)
		theme.SetHasDarkBackground(oldDark)
		restoreMatrixTheme(oldTheme)
	})
	root := t.TempDir()
	cfg.WorkingDir = root
	cfg.Data.Directory = t.TempDir()
	cfg.TUI.Language = "en"
	cfg.MCPServers = nil
	cfg.LSP = nil
	cfg.Providers = map[models.ModelProvider]config.Provider{models.ProviderLocal: {BaseURL: "http://matrix.invalid/v1", API: "ollama"}}
	cfg.ProviderProfiles = nil
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentCoder: {Model: models.GPT41}}
	beforeTransport := http.DefaultTransport
	http.DefaultTransport = matrixCatalogTransport{}
	t.Cleanup(func() { http.DefaultTransport = beforeTransport })

	file := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(file, []byte(`{}`), 0600))
	viper.SetConfigFile(file)
	testRoutedModalRegressionMatrixStruct(t)
}
func matrixRoot(size [2]int) (appModel, *matrixPageProbe) {
	probe := &matrixPageProbe{content: "Matrix retained composer"}
	application := &app.App{CoderAgent: &terminalAgent{}, Messages: &terminalMessages{}, Sessions: &terminalSessions{}, Permissions: permission.NewPermissionService()}
	a := appModel{app: application, currentPage: page.ChatPage, pages: map[page.PageID]util.Model{page.ChatPage: probe}, status: core.NewStatusCmp(nil), help: dialog.NewHelpCmp(), quit: dialog.NewQuitCmp(), sessionDialog: dialog.NewSessionDialogCmp(), commandDialog: dialog.NewCommandDialogCmp(), modelDialog: dialog.NewModelDialogCmp(), permissions: dialog.NewPermissionDialogCmp(), initDialog: dialog.NewInitDialogCmp(), themeDialog: dialog.NewThemeDialogCmp(), providerDialog: dialog.NewProviderDialogCmp(), integrationDialog: dialog.NewIntegrationDialogCmp(nil), filepicker: dialog.NewFilepickerCmp(application)}
	a.help.SetBindings([]key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "send")), key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("Ctrl+P", "terminal"))})
	u, _ := a.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	return u.(appModel), probe
}
func openMatrixModal(t *testing.T, a *appModel, name string) {
	switch name {
	case "commands", "mode":
		a.commandDialog.SetCommands([]dialog.Command{{ID: "matrix-first", Title: "Matrix first"}, {ID: "matrix-second", Title: "Matrix second"}, {ID: "matrix-third", Title: "Matrix third"}})
		if name == "mode" {
			a.commandDialog.(interface{ SetContext(string, string) }).SetContext("Work mode", "matrix-first")
		}
		a.showCommandDialog = true
	case "permissions":
		a.permissions.SetPermissions(permission.PermissionRequest{ID: "matrix-request", SessionID: "matrix-session", ToolName: "matrix-tool", Path: "/matrix", Params: map[string]string{"operation": "read fixture"}})
		a.showPermissions = true
	case "quit":
		a.showQuit = true
	case "init":
		a.showInitDialog = true
	case "sessions":
		a.sessionDialog.SetSessions([]session.Session{{ID: "matrix-a", Title: "Matrix session A"}, {ID: "matrix-b", Title: "Matrix session B"}})
		a.showSessionDialog = true
	case "arguments":
		a.multiArgumentsDialog = dialog.NewMultiArgumentsDialogCmp("matrix-command", "", []string{"first", "second", "third"})
		a.showMultiArgumentsDialog = true
		a.multiArgumentsDialog.SetSize(a.width, a.height)
	case "files":
		a.showFilepicker = true
		a.filepicker.ToggleFilepicker(true)
	case "themes":
		a.themeDialog.Init()
		a.showThemeDialog = true
	case "help":
		a.showHelp = true
	case "connections":
		a.providerDialog.Open()
		a.showProviderDialog = true
	case "integrations":
		a.integrationDialog.Open()
		a.showIntegrationDialog = true
	case "models":
		cmd := a.modelDialog.Open()
		if cmd != nil {
			u, _ := a.Update(cmd())
			*a = u.(appModel)
		}
		a.showModelDialog = true
		t.Cleanup(a.modelDialog.Close)
	}
}
func matrixClick(t *testing.T, a appModel, label string) (appModel, tea.Cmd) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(a.View().Content), "\n") {
		if before, _, ok := strings.Cut(line, label); ok {
			u, cmd := a.Update(tea.MouseClickMsg{X: ansi.StringWidth(before) + 1, Y: y, Button: tea.MouseLeft})
			return u.(appModel), cmd
		}
	}
	t.Fatalf("click label %q absent:\n%s", label, ansi.Strip(a.View().Content))
	return a, nil
}
func assertMatrixFrame(t *testing.T, a appModel, size [2]int) {
	t.Helper()
	require.LessOrEqual(t, lipgloss.Width(a.View().Content), size[0])
	require.LessOrEqual(t, lipgloss.Height(a.View().Content), size[1])
}

func themeNameForMatrix() string     { return theme.CurrentThemeName() }
func restoreMatrixTheme(name string) { _ = theme.PreviewTheme(name) }

type matrixPageProbe struct{ focusPageProbe }

func (p *matrixPageProbe) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	p.received = append(p.received, msg)
	return p, nil
}

type matrixCatalogTransport struct{}

func (matrixCatalogTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "matrix.invalid" {
		return nil, fmt.Errorf("matrix refused unexpected endpoint")
	}
	body := `{}`
	status := 404
	if req.URL.Path == "/v1/models" {
		body = `{"data":[{"id":"matrix-small"},{"id":"matrix-large"}]}`
		status = 200
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func testRoutedModalRegressionMatrixStruct(t *testing.T) {
	for _, profile := range []struct {
		name  string
		dark  bool
		color colorprofile.Profile
	}{{"dark", true, colorprofile.TrueColor}, {"light", false, colorprofile.TrueColor}, {"NO_COLOR", false, colorprofile.Ascii}} {
		t.Run(profile.name, func(t *testing.T) {
			theme.SetHasDarkBackground(profile.dark)
			theme.SetColorProfile(profile.color)
			testRoutedModalRegressionMatrixStructState(t)
		})
	}
}

func testRoutedModalRegressionMatrixStructState(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {120, 36}, {180, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			testRoutedModalRegressionMatrixStructStateState(&size, t)
		})
	}
}

func testRoutedModalRegressionMatrixStructStateState(size *[2]int, t *testing.T) {
	for _, name := range []string{"commands", "mode", "permissions", "quit", "init", "sessions", "arguments", "files", "themes", "help", "connections", "integrations", "models"} {
		t.Run(name, func(t *testing.T) {
			a, probe := matrixRoot((*size))
			openMatrixModal(t, &a, name)
			a.syncInputFocus()
			require.False(t, probe.focused, "modal must suspend composer focus")
			assertMatrixFrame(t, a, (*size))
			u, _ := a.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
			a = u.(appModel)
			assertMatrixFrame(t, a, [2]int{60, 20})
			u, _ = a.Update(tea.WindowSizeMsg{Width: (*size)[0], Height: (*size)[1]})
			a = u.(appModel)
			switch name {
			case "commands", "mode":
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: 0})
				a = u.(appModel)
				if name == "mode" {
					before := a.commandDialog.View()
					u, _ = a.Update(testTextKey(string([]rune("invisible filter")), 0))
					a = u.(appModel)
					require.Equal(t, before, a.commandDialog.View())
				}
				_, cmd := matrixClick(t, a, "Matrix second")
				require.NotNil(t, cmd)
				chosen := cmd().(dialog.CommandSelectedMsg)
				require.Equal(t, "matrix-second", chosen.Command.ID)
				u, _ = a.Update(chosen)
				a = u.(appModel)
				require.False(t, a.showCommandDialog)
			case "permissions":
				u, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				require.NotNil(t, cmd)
				denial := cmd().(dialog.PermissionResponseMsg)
				require.Equal(t, dialog.PermissionDeny, denial.Action)
				require.Equal(t, "matrix-request", denial.Permission.ID)
				u, _ = a.Update(denial)
				a = u.(appModel)
			case "quit":
				var cmd tea.Cmd
				a, cmd = matrixClick(t, a, "Continue")
				require.NotNil(t, cmd)
				u, _ = a.Update(cmd())
				a = u.(appModel)
				require.False(t, a.showQuit)
			case "sessions":
				a, _ = matrixClick(t, a, "Matrix session B")
				_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: 0})
				require.NotNil(t, cmd)
				selected := cmd().(dialog.SessionSelectedMsg)
				require.Equal(t, "matrix-b", selected.Session.ID)
				u, _ = a.Update(selected)
				a = u.(appModel)
				require.False(t, a.showSessionDialog)
			case "arguments":
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: 0})
				a = u.(appModel)
				u, _ = a.Update(testTextKey(string([]rune("ação 漢字")), 0))
				a = u.(appModel)
				require.Contains(t, ansi.Strip(a.View().Content), "ação 漢字")
				u, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				require.NotNil(t, cmd)
				result := cmd().(dialog.CloseMultiArgumentsDialogMsg)
				require.False(t, result.Submit)
				u, _ = a.Update(result)
				a = u.(appModel)
			case "files":
				require.Contains(t, ansi.Strip(a.View().Content), "Empty directory")
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: 0})
				a = u.(appModel)
				u, _ = a.Update(testTextKey(string([]rune("l")), 0))
				a = u.(appModel)
				require.True(t, a.showFilepicker)
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				require.False(t, a.showFilepicker)
			case "themes":
				before := a.themeDialog.View()
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: 0})
				a = u.(appModel)
				require.NotEqual(t, before, a.themeDialog.View())
				require.Contains(t, ansi.Strip(a.themeDialog.View()), "›")
				u, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				require.NotNil(t, cmd)
				u, _ = a.Update(cmd())
				a = u.(appModel)
				require.False(t, a.showThemeDialog)
			case "help":
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: 0})
				a = u.(appModel)
				a, _ = matrixClick(t, a, "Keyboard shortcuts")
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				require.False(t, a.showHelp)
			case "connections":
				a, _ = matrixClick(t, a, "[Preferences]")
				require.Contains(t, ansi.Strip(a.View().Content), "Preferences")
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				u, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				require.NotNil(t, cmd)
				u, _ = a.Update(cmd())
				a = u.(appModel)
				require.False(t, a.showProviderDialog)
			case "integrations":
				u, _ = a.Update(testTextKey(string([]rune("n")), 0))
				a = u.(appModel)
				require.Contains(t, ansi.Strip(a.View().Content), "Configure MCP")
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: 0})
				a = u.(appModel)
				u, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				u, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				require.NotNil(t, cmd)
				u, _ = a.Update(cmd())
				a = u.(appModel)
				require.False(t, a.showIntegrationDialog)
			case "models":
				u, _ = a.Update(testTextKey(string([]rune("no-such-matrix-model")), 0))
				a = u.(appModel)
				require.Contains(t, ansi.Strip(a.View().Content), "No models found")
				u, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
				a = u.(appModel)
				require.NotNil(t, cmd)
				u, _ = a.Update(cmd())
				a = u.(appModel)
				require.False(t, a.showModelDialog)
			case "init":
				// Initialization is refused by mouse. Never ask the agent to create files.
				var cmd tea.Cmd
				a, cmd = matrixClick(t, a, "Not now")
				require.NotNil(t, cmd)
				result := cmd().(dialog.CloseInitDialogMsg)
				require.False(t, result.Initialize)
				u, _ = a.Update(result)
				a = u.(appModel)
				require.False(t, a.showInitDialog)
			}
			a.syncInputFocus()
			require.True(t, probe.focused, "closing must return composer focus")
			for _, msg := range probe.received {
				_, leaked := msg.(tea.KeyPressMsg)
				require.False(t, leaked, "modal key leaked to background composer")
			}
			assertMatrixFrame(t, a, (*size))
		})
	}
}
