package dialog

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
)

// Known enums are edited as choices. Persisted values and validation remain in
// config, so presentation cannot silently introduce new provider capabilities.
func fieldOptions(label string) []string {
	if strings.Contains(label, "(true/false)") {
		return []string{"false", "true"}
	}
	switch {
	case label == "Authentication":
		return []string{"none", "api_key", "environment", "vault"}
	case strings.HasPrefix(label, "Backend ("):
		return []string{"local", "openai", "anthropic", "gemini", "groq", "openrouter", "xai"}
	case strings.HasPrefix(label, "API ("):
		return []string{"", "chat_completions", "responses", "ollama"}
	case strings.HasPrefix(label, "Initial mode"):
		return []string{"ask", "plan", "execute"}
	case strings.HasPrefix(label, "Transport ("):
		return []string{"stdio", "http", "sse"}
	}
	return nil
}
func settingOptions(path string) []string {
	switch path {
	case "tui.language":
		return []string{"en", "pt"}
	case "tui.sidebar":
		return []string{"auto", "shown", "hidden"}
	case "tui.notifications":
		return []string{"off", "bell", "auto", "native", "osc"}
	case "runtime.mode":
		return []string{"ask", "plan", "execute"}
	case "preferences.resumeLast", "preferences.showProcess", "runtime.webSearch":
		return []string{"false", "true"}
	}
	return nil
}
func choiceKey(field *textinput.Model, options []string, msg tea.Msg) bool {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || len(options) == 0 {
		return false
	}
	step := 0
	switch k.String() {
	case "left":
		step = -1
	case "right", "space":
		step = 1
	default:
		// Navigation and save/cancel belong to the surrounding form.
		return k.Text != "" || k.Code == tea.KeyBackspace || k.Code == tea.KeyDelete
	}
	i := max(slices.Index(options, field.Value()), 0)
	field.SetValue(options[(i+step+len(options))%len(options)])
	return true
}
func choiceView(field textinput.Model, options []string) string {
	if len(options) == 0 {
		return field.View()
	}
	return "‹ " + choiceLabel(field.Value()) + " ›  " + config.Text("←/→ escolher", "←/→ choose")
}

// choiceRadios shows every option as a radio, "● On  ○ Off", when they fit
// the width; otherwise the compact ‹ value › stepper.
func choiceRadios(field textinput.Model, options []string, width int) string {
	if len(options) == 0 {
		return field.View()
	}
	var parts []string
	for _, option := range options {
		mark := "○ "
		if option == field.Value() {
			mark = "● "
		}
		parts = append(parts, mark+choiceLabel(option))
	}
	if line := strings.Join(parts, "   "); ansi.StringWidth(line) <= width {
		return line
	}
	return choiceView(field, options)
}

func choiceLabel(v string) string {
	switch v {
	case "none":
		v = config.Text("Sem autenticação", "No authentication")
	case "api_key":
		v = config.Text("Chave direta", "Direct API key")
	case "vault":
		v = config.Text("Cofre do sistema", "System credential vault")
	case "environment":
		v = config.Text("Variável de ambiente", "Environment variable")
	case "true":
		v = config.Text("Ligado", "On")
	case "false":
		v = config.Text("Desligado", "Off")
	case "":
		v = config.Text("Automático", "Automatic")
	}
	return v
}
func fieldLabel(label string) string {
	if p := strings.Index(label, " ("); p >= 0 && len(fieldOptions(label)) > 0 {
		label = label[:p]
	}
	return interfaceCopy(label)
}
