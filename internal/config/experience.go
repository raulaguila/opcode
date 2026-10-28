package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/raulaguila/opcode/internal/llm/models"
)

// Text localizes interface copy without modifying user or model content.
func Text(portuguese, english string) string {
	if cfg != nil && cfg.TUI.Language == "pt" {
		return portuguese
	}
	return english
}
func (t TUIConfig) SidebarVisible(width int) bool {
	return t.Sidebar != "hidden" && width >= 110 && (t.Sidebar == "shown" || width >= 140)
}

var DefaultKeybindings = map[string]string{"commands": "ctrl+k", "models": "ctrl+o", "sessions": "ctrl+s", "terminal": "ctrl+p", "files": "ctrl+f", "theme": "ctrl+t", "sidebar": "alt+b"}

func Shortcut(action string) string {
	if cfg != nil && cfg.TUI.Keybindings[action] != "" {
		return cfg.TUI.Keybindings[action]
	}
	return DefaultKeybindings[action]
}
func (t TUIConfig) Validate() error {
	if t.Sidebar != "" && t.Sidebar != "auto" && t.Sidebar != "shown" && t.Sidebar != "hidden" {
		return fmt.Errorf("sidebar: auto/shown/hidden")
	}
	if t.Language != "" && t.Language != "pt" && t.Language != "en" {
		return fmt.Errorf("language: pt/en")
	}
	if t.Notifications != "" && t.Notifications != "off" && t.Notifications != "bell" {
		return fmt.Errorf("notifications: off/bell")
	}
	seen := map[string]bool{}
	for action, fallback := range DefaultKeybindings {
		shortcut := t.Keybindings[action]
		if shortcut == "" {
			shortcut = fallback
		}
		reserved := map[string]bool{"ctrl+c": true, "ctrl+h": true, "ctrl+l": true, "ctrl+n": true, "ctrl+e": true, "ctrl+b": true, "ctrl+y": true, "ctrl+r": true}
		if reserved[shortcut] || !validShortcut(shortcut) {
			return fmt.Errorf("invalid shortcut for %s: use a free ctrl+letter or alt+letter", action)
		}
		if seen[shortcut] {
			return fmt.Errorf("shortcut %s is assigned twice", shortcut)
		}
		seen[shortcut] = true
	}
	for action := range t.Keybindings {
		if _, ok := DefaultKeybindings[action]; !ok {
			return fmt.Errorf("unknown shortcut action %s", action)
		}
	}
	return nil
}
func validShortcut(s string) bool {
	p := strings.Split(s, "+")
	return len(p) == 2 && (p[0] == "ctrl" || p[0] == "alt") && len(p[1]) == 1 && p[1][0] >= 'a' && p[1][0] <= 'z'
}

// Setting describes an editable user preference, including its effective origin.
// Provider credentials are exposed only through EffectiveConfiguration, redacted.
type Setting struct {
	Path, Category, Label, Help string
	Default                     any
	Advanced                    bool
}

var UserSettings = []Setting{
	{"tui.sidebar", "interface", "Lateral / Sidebar", "auto, shown, hidden", "auto", false},
	{"tui.language", "interface", "Idioma / Language", "pt, en", "en", false},
	{"tui.notifications", "interface", "Notificações / Notifications", "off, bell (only when unfocused)", "off", false},
	{"preferences.resumeLast", "sessions", "Retomar conversa / Resume last session", "true, false", false, false},
	{"preferences.showProcess", "interface", "Mostrar processo / Show process", "true, false", true, false},
	{"runtime.mode", "execution", "Modo inicial / Initial mode", "ask, plan, execute", "execute", false},
	{"runtime.maxSteps", "execution", "Limite de etapas / Step limit", "1–200", 32, true},
	{"runtime.maxSeconds", "execution", "Tempo máximo / Time limit", "1–7200 seconds", 1800, true},
	{"runtime.toolBudget", "execution", "Saída de ferramentas / Tool output budget", "4096–1048576 bytes", 96000, true},
	{"runtime.firstTokenTimeout", "execution", "Primeiro token / First token timeout", "1–1800 seconds", 300, true},
	{"runtime.idleTimeout", "execution", "Inatividade / Idle timeout", "1–600 seconds", 90, true},
}

func init() {
	keys := make([]string, 0, len(DefaultKeybindings))
	for k := range DefaultKeybindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		UserSettings = append(UserSettings, Setting{"tui.keybindings." + k, "shortcuts", k, "ctrl+letter, alt+letter", DefaultKeybindings[k], true})
	}
}

type EffectiveValue struct {
	Path   string
	Value  any
	Origin string
	Secret bool
}

const maxSettingsDocument = 4 << 20

func readSettingsDocument(path string) (map[string]any, error) {
	empty := map[string]any{}
	for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
		stat, e := os.Lstat(parent)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return nil, fmt.Errorf("cannot inspect configuration parent")
		}
		if e == nil && (!stat.IsDir() || stat.Mode()&os.ModeSymlink != 0) {
			return nil, fmt.Errorf("configuration parents must not be symlinks")
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot inspect configuration")
	}
	if !info.Mode().IsRegular() || info.Size() > maxSettingsDocument {
		return nil, fmt.Errorf("configuration must be a regular file under 4 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read configuration")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("configuration changed while reading")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSettingsDocument+1))
	if err != nil || len(data) > maxSettingsDocument {
		return nil, fmt.Errorf("configuration exceeds 4 MiB")
	}
	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			offset := min(len(data), max(0, int(syntax.Offset)-1))
			line := bytes.Count(data[:offset], []byte("\n")) + 1
			column := offset - bytes.LastIndexByte(data[:offset], '\n')
			return nil, fmt.Errorf("invalid configuration JSON object at line %d, column %d (values omitted)", line, column)
		}
		return nil, fmt.Errorf("invalid configuration JSON object (values omitted)")
	}
	if doc == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("invalid configuration JSON object; expected exactly one object")
	}
	return doc, nil
}
func configDocument(path string) map[string]any {
	doc, _ := readSettingsDocument(path)
	if doc == nil {
		return map[string]any{}
	}
	return doc
}
func environmentSettingOrigin(path string) string {
	upper := strings.ToUpper(path)
	underscored := strings.ReplaceAll(upper, ".", "_")
	for _, name := range []string{"OPCODE_" + upper, "OPCODE_" + underscored, "OPENCODE_" + upper, "OPENCODE_" + underscored} {
		if _, ok := os.LookupEnv(name); ok {
			return name
		}
	}
	return ""
}
func referenceEnvironment(name string) string {
	for _, alias := range EnvironmentAliases(name) {
		if _, ok := os.LookupEnv(alias); ok {
			return alias
		}
	}
	return ""
}

func atPath(doc map[string]any, path string) (any, bool) {
	var v any = doc
	for _, p := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return v, true
}
func settingDefault(path string) any {
	for _, s := range UserSettings {
		if s.Path == path {
			return s.Default
		}
	}
	return nil
}
func EffectiveConfiguration() []EffectiveValue {
	if cfg == nil {
		return nil
	}
	data, _ := json.Marshal(cfg)
	var doc map[string]any
	_ = json.Unmarshal(data, &doc)
	global := configDocument(configuredFilePath())
	project := configDocument(ProjectConfigPath(cfg.WorkingDir))
	out := []EffectiveValue{}
	var walk func(map[string]any, string)
	walk = func(m map[string]any, prefix string) {
		for k, v := range m {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if nested, ok := v.(map[string]any); ok && !sensitiveName(k) {
				walk(nested, path)
				continue
			}
			origin := "default"
			if _, ok := atPath(global, path); ok {
				origin = "global"
			}
			if _, ok := atPath(project, path); ok && !strings.HasPrefix(path, "security.") && !strings.HasPrefix(path, "extensions.") {
				origin = "project"
			}
			secret := sensitiveName(k) || k == "args"
			value := v
			if secret {
				if v != nil && fmt.Sprint(v) != "" {
					value = "[redacted]"
				}
			} else if s, ok := v.(string); ok {
				value = safeValue(s)
			}
			out = append(out, EffectiveValue{path, value, origin, secret})
		}
	}
	walk(doc, "")
	for _, s := range UserSettings {
		if _, ok := atPath(doc, s.Path); !ok {
			origin := "default"
			if _, ok := atPath(global, s.Path); ok {
				origin = "global"
			}
			if _, ok := atPath(project, s.Path); ok {
				origin = "project"
			}
			out = append(out, EffectiveValue{s.Path, s.Default, origin, false})
		}
	}
	// Environment references are attributed without revealing resolved values.
	for i := range out {
		e := &out[i]
		if name := environmentSettingOrigin(e.Path); name != "" {
			e.Origin = "environment (" + name + ")"
		}
		if strings.HasPrefix(e.Path, "providerProfiles.") || strings.HasPrefix(e.Path, "providers.") {
			parts := strings.Split(e.Path, ".")
			if len(parts) == 3 && parts[2] == "apiKey" {
				prefix := parts[0] + "." + parts[1]
				if ref, ok := atPath(doc, prefix+".apiKeyEnv"); ok && ref != "" {
					if name := referenceEnvironment(fmt.Sprint(ref)); name != "" {
						e.Origin = "environment (" + name + ")"
					}
				} else if parts[0] == "providers" {
					if name := referenceEnvironment(ProviderEnvironmentName(models.ModelProvider(parts[1]))); name != "" {
						e.Origin = "environment (" + name + ")"
					}
				}
			}
			if e.Path == "providers.local.baseURL" && referenceEnvironment("LOCAL_ENDPOINT") != "" {
				e.Origin = "environment (LOCAL_ENDPOINT)"
			}
		}
	}
	for _, p := range []string{configuredFilePath(), ProjectConfigPath(cfg.WorkingDir)} {
		if _, err := readSettingsDocument(p); err != nil {
			out = append(out, EffectiveValue{"configuration", err.Error(), "error", false})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func EffectiveSetting(path string) EffectiveValue {
	for _, v := range EffectiveConfiguration() {
		if v.Path == path {
			return v
		}
	}
	return EffectiveValue{path, settingDefault(path), "default", false}
}
func sensitiveName(k string) bool {
	k = strings.ToLower(k)
	return k == "apikey" || k == "authorization" || k == "headers" || k == "env" || k == "envvars" || strings.Contains(k, "secret") || strings.Contains(k, "password") || k == "access_token" || k == "refresh_token" || k == "token"
}
func safeValue(s string) string {
	if u, e := url.Parse(s); e == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		if u.RawQuery != "" {
			u.RawQuery = "[redacted]"
		}
		u.Fragment = ""
		s = u.String()
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// SaveUserSetting patches a single allowlisted preference in its owning layer.
// Reset removes that layer's override and reveals the lower layer/default.
func SaveUserSetting(path, input string, reset bool) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	var setting *Setting
	for i := range UserSettings {
		if UserSettings[i].Path == path {
			setting = &UserSettings[i]
			break
		}
	}
	if setting == nil {
		return fmt.Errorf("setting is not editable")
	}
	globalPath := configuredFilePath()
	projectPath := ProjectConfigPath(cfg.WorkingDir)
	global, err := readSettingsDocument(globalPath)
	if err != nil {
		return err
	}
	project, err := readSettingsDocument(projectPath)
	if err != nil {
		return err
	}
	if name := environmentSettingOrigin(path); name != "" {
		return fmt.Errorf("setting is controlled by environment %s; remove that override before editing", name)
	}
	targetPath := globalPath
	target := global
	if _, ok := atPath(project, path); ok {
		targetPath, target = projectPath, project
	}
	originalDocument, _ := json.Marshal(target)
	var value any
	if !reset {
		switch setting.Default.(type) {
		case bool:
			if input != "true" && input != "false" {
				return fmt.Errorf("use true or false")
			}
			value = input == "true"
		case int:
			var n int
			if _, e := fmt.Sscanf(input, "%d", &n); e != nil || fmt.Sprint(n) != strings.TrimSpace(input) {
				return fmt.Errorf("use an integer")
			}
			value = n
		default:
			value = strings.TrimSpace(input)
		}
	}
	parts := strings.Split(path, ".")
	node := target
	for _, p := range parts[:len(parts)-1] {
		child, ok := node[p].(map[string]any)
		if !ok {
			child = map[string]any{}
			node[p] = child
		}
		node = child
	}
	if reset {
		delete(node, parts[len(parts)-1])
	} else {
		node[parts[len(parts)-1]] = value
	}
	// Compute only the changed preference. Do not reload providers or discover models.
	if reset {
		value = setting.Default
		if v, ok := atPath(global, path); ok && targetPath == projectPath {
			value = v
		}
	}
	raw, _ := json.Marshal(cfg)
	var candidate map[string]any
	_ = json.Unmarshal(raw, &candidate)
	node = candidate
	for _, p := range parts[:len(parts)-1] {
		child, ok := node[p].(map[string]any)
		if !ok {
			child = map[string]any{}
			node[p] = child
		}
		node = child
	}
	node[parts[len(parts)-1]] = value
	raw, _ = json.Marshal(candidate)
	var next Config
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if err := next.TUI.Validate(); err != nil {
		return err
	}
	if err := next.Runtime.Validate(); err != nil {
		return err
	}
	currentDocument, err := readSettingsDocument(targetPath)
	if err != nil {
		return err
	}
	currentJSON, _ := json.Marshal(currentDocument)
	if !bytes.Equal(originalDocument, currentJSON) {
		return fmt.Errorf("configuration changed while editing; reopen settings")
	}
	data, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(targetPath), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(targetPath), ".opcode-preferences-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if current, e := os.Lstat(targetPath); e == nil && (!current.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("configuration changed before saving")
	}
	if err = os.Rename(temp, targetPath); err != nil {
		return err
	}
	cfg.TUI = next.TUI
	cfg.Preferences = next.Preferences
	cfg.Runtime = next.Runtime.Defaults()
	return nil
}

type ProviderPreset struct {
	ID      string
	Profile ProviderProfile
}

var ProviderPresets = []ProviderPreset{
	{"ollama", ProviderProfile{Name: "Ollama", Backend: models.ProviderLocal, Provider: Provider{BaseURL: "http://127.0.0.1:11434/v1", API: "ollama", ContextWindow: 16384, DefaultMaxTokens: 2048}}},
	{"lmstudio", ProviderProfile{Name: "LM Studio", Backend: models.ProviderLocal, Provider: Provider{BaseURL: "http://127.0.0.1:1234/v1", API: "chat_completions", ContextWindow: 16384, DefaultMaxTokens: 2048}}},
	{"openai", ProviderProfile{Name: "OpenAI", Backend: models.ProviderOpenAI, Provider: Provider{APIKeyEnv: "OPENAI_API_KEY"}}},
	{"anthropic", ProviderProfile{Name: "Anthropic", Backend: models.ProviderAnthropic, Provider: Provider{APIKeyEnv: "ANTHROPIC_API_KEY"}}},
	{"gemini", ProviderProfile{Name: "Gemini", Backend: models.ProviderGemini, Provider: Provider{APIKeyEnv: "GEMINI_API_KEY"}}},
	{"openrouter", ProviderProfile{Name: "OpenRouter", Backend: models.ProviderOpenRouter, Provider: Provider{APIKeyEnv: "OPENROUTER_API_KEY"}}},
}
