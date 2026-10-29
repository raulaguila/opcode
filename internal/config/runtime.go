package config

import "fmt"

// RuntimeConfig bounds one agent turn, including queued instructions.
type RuntimeConfig struct {
	WebSearch         bool   `json:"webSearch,omitempty"`
	Mode              string `json:"mode,omitempty"`
	MaxSteps          int    `json:"maxSteps,omitempty"`
	MaxSeconds        int    `json:"maxSeconds,omitempty"`
	ToolBudget        int    `json:"toolBudget,omitempty"`
	FirstTokenTimeout int    `json:"firstTokenTimeout,omitempty"`
	IdleTimeout       int    `json:"idleTimeout,omitempty"`
}

func (r RuntimeConfig) Defaults() RuntimeConfig {
	if r.Mode == "" {
		r.Mode = "execute"
	}
	if r.MaxSteps == 0 {
		r.MaxSteps = 32
	}
	if r.MaxSeconds == 0 {
		r.MaxSeconds = 1800
	}
	if r.ToolBudget == 0 {
		r.ToolBudget = 96000
	}
	if r.FirstTokenTimeout == 0 {
		r.FirstTokenTimeout = 300
	}
	if r.IdleTimeout == 0 {
		r.IdleTimeout = 90
	}
	return r
}

func (r RuntimeConfig) Validate() error {
	r = r.Defaults()
	if r.Mode != "ask" && r.Mode != "plan" && r.Mode != "execute" {
		return fmt.Errorf("runtime.mode must be ask, plan or execute")
	}
	if r.MaxSteps < 1 || r.MaxSteps > 200 || r.MaxSeconds < 1 || r.MaxSeconds > 7200 || r.ToolBudget < 4096 || r.ToolBudget > 1048576 || r.FirstTokenTimeout < 1 || r.FirstTokenTimeout > 1800 || r.IdleTimeout < 1 || r.IdleTimeout > 600 {
		return fmt.Errorf("invalid runtime limits: maxSteps 1–200, maxSeconds 1–7200, toolBudget 4096–1048576, firstTokenTimeout 1–1800 and idleTimeout 1–600 (seconds)")
	}
	return nil
}
