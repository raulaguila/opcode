package core

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (m statusCmp) modeLabel(state util.FooterState) string {
	mode := state.Mode
	if mode == "" && config.Get() != nil {
		mode = config.Get().Runtime.Defaults().Mode
	}
	switch mode {
	case "ask":
		if m.width < 80 {
			return config.Text("Perg", "Ask")
		}
		return config.Text("Perguntar", "Ask")
	case "plan":
		if m.width < 80 {
			return "Plan"
		}
		return config.Text("Planejar", "Plan")
	}
	if m.width < 80 {
		return "Exec"
	}
	return config.Text("Executar", "Execute")
}

func (m statusCmp) approvalLabel(state util.FooterState) string {
	if m.width >= 160 {
		if state.ApprovalPolicy == "sandboxed" {
			return config.Text("Aprovação: pedir aprovação", "Approval: ask for approval")
		}
		return config.Text("Aprovação: confirmar tudo", "Approval: confirm all")
	}
	// One concept, one name: menus call it Aprovação, so the footer abbreviates that.
	prefix := "A:"
	if m.width >= 110 {
		prefix = config.Text("Aprov.: ", "Appr.: ")
	}
	if state.ApprovalPolicy == "sandboxed" {
		return prefix + config.Text("pedir", "ask")
	}
	return prefix + config.Text("confirmar", "confirm")
}

func (m statusCmp) controlWidths(state util.FooterState) (mode, approval int) {
	padding := 3 // Leading space and the spaced dropdown arrow.
	if m.width < 60 {
		padding = 2
	}
	available := max(0, m.width-1)
	// The mode lives in the composer's frame; the footer keeps approval.
	approval = min(ansi.StringWidth(m.approvalLabel(state))+padding, available)
	return
}
