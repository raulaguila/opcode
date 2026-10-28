package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ExtensionsConfig is trusted user-global configuration. Projects cannot enable
// executable hooks or add skill search paths.
type ExtensionsConfig struct {
	Skills []string     `json:"skills,omitempty"`
	Hooks  []HookConfig `json:"hooks,omitempty"`
}
type HookConfig struct {
	Event          string `json:"event"`
	Tool           string `json:"tool,omitempty"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

func (c ExtensionsConfig) Validate() error {
	if len(c.Skills) > 16 || len(c.Hooks) > 16 {
		return fmt.Errorf("extensions allow at most 16 skill directories and 16 hooks")
	}
	for _, path := range c.Skills {
		if !filepath.IsAbs(path) || len(path) > 4096 {
			return fmt.Errorf("skill directories must be absolute paths")
		}
	}
	for _, hook := range c.Hooks {
		if hook.Event != "before_tool" && hook.Event != "after_tool" {
			return fmt.Errorf("hook event must be before_tool or after_tool")
		}
		if strings.TrimSpace(hook.Command) == "" || len(hook.Command) > 4096 {
			return fmt.Errorf("hook command must contain 1–4096 bytes")
		}
		if hook.TimeoutSeconds < 0 || hook.TimeoutSeconds > 30 {
			return fmt.Errorf("hook timeout_seconds must be between 0 and 30")
		}
		if len(hook.Tool) > 100 || strings.ContainsAny(hook.Tool, "\n\r\x00") {
			return fmt.Errorf("invalid hook tool filter")
		}
	}
	return nil
}
