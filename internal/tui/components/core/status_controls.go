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
		if state.ApprovalPolicy == "project_edits" {
			return config.Text("Aprovação: editar no projeto", "Approval: project edits")
		}
		return config.Text("Aprovação: pedir", "Approval: ask")
	}
	prefix := "P:"
	if m.width >= 110 {
		prefix = "Perm.: "
	}
	if state.ApprovalPolicy == "project_edits" {
		return prefix + config.Text("editar", "edit")
	}
	return prefix + config.Text("pedir", "ask")
}

func (m statusCmp) controlWidths(state util.FooterState) (mode, approval int) {
	padding := 3 // Leading space and the spaced dropdown arrow.
	if m.width < 60 {
		padding = 2
	}
	available := max(0, m.width-1)
	mode = min(ansi.StringWidth(m.modeLabel(state))+padding, available)
	approval = min(ansi.StringWidth(m.approvalLabel(state))+padding, available-mode)
	return
}
