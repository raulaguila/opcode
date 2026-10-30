package dialog

import (
	"errors"
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type providerSaveResult struct {
	destination models.ModelProvider
	disabled    bool
	pending     bool
	command     tea.Cmd
	err         error
}

func (m *providerDialogCmp) persistSettings() providerSaveResult {
	switch m.screen {
	case "provider":
		return m.persistProvider()
	case "preferences":
		return providerSaveResult{err: m.persistPreferences()}
	default:
		return providerSaveResult{}
	}
}

func (m *providerDialogCmp) persistProvider() providerSaveResult {
	id, profile, err := m.readProfile()
	result := providerSaveResult{destination: models.ModelProvider("profile:" + id), disabled: profile.Disabled, err: err}
	if m.legacy {
		result.destination = profile.Backend
	}
	if err != nil {
		return result
	}
	if m.fields[providerAuthField].Value() == "vault" {
		m.Close()
		m.saving = true
		result.pending, result.command = true, m.prepareVaultSave(id, profile)
		return result
	}
	if m.legacy {
		result.err = config.SaveProvider(profile.Backend, profile.Provider)
	} else {
		result.err = config.SaveProviderProfile(id, profile)
	}
	return result
}

func (m *providerDialogCmp) prepareVaultSave(id string, profile config.ProviderProfile) tea.Cmd {
	secret, prepare := m.fieldValue(providerKeyField), m.prepareVault
	return func() tea.Msg {
		prepared, err := prepare(profile.APIKeyRef, secret)
		return providerVaultSaveMsg{id: id, profile: profile, prepared: prepared, err: err}
	}
}

func (m *providerDialogCmp) persistPreferences() error {
	cfg := config.Get()
	runtime, preferences := cfg.Runtime, cfg.Preferences
	runtime.Mode = m.fieldValue(0)
	limits := make([]int, 5)
	for i := range limits {
		value, err := strconv.Atoi(m.fieldValue(i + 1))
		if err != nil || value < 1 {
			return fmt.Errorf("limits must be positive integers")
		}
		limits[i] = value
	}
	runtime.MaxSteps, runtime.MaxSeconds, runtime.ToolBudget = limits[0], limits[1], limits[2]
	runtime.FirstTokenTimeout, runtime.IdleTimeout = limits[3], limits[4]
	resume, err := strconv.ParseBool(m.fieldValue(6))
	if err != nil {
		return err
	}
	visible, err := strconv.ParseBool(m.fieldValue(7))
	if err != nil {
		return err
	}
	preferences.ResumeLast, preferences.ShowProcess = resume, &visible
	return config.SavePreferences(preferences, runtime)
}

func rollbackVaultCommand(prepared *config.PreparedVaultCredential, cause error) tea.Cmd {
	return func() tea.Msg { return providerVaultRollbackMsg{err: errors.Join(cause, prepared.Rollback())} }
}

func (m *providerDialogCmp) finishVaultSave(result providerVaultSaveMsg) tea.Cmd {
	if !m.saving {
		if result.prepared == nil {
			return nil
		}
		m.saving = true
		return rollbackVaultCommand(result.prepared, errors.New("stale vault save discarded; previous configuration preserved"))
	}
	err := result.err
	if err == nil && result.prepared == nil {
		err = errors.New("native credential was not prepared")
	}
	if err == nil && m.allowSave != nil {
		err = m.allowSave()
	}
	destination := models.ModelProvider("profile:" + result.id)
	if err == nil {
		destination, err = m.commitVaultMetadata(result)
	}
	if err != nil {
		if result.prepared != nil {
			return rollbackVaultCommand(result.prepared, err)
		}
		m.saving, m.note = false, err.Error()
		return nil
	}
	m.saving = false
	m.Close()
	return util.CmdHandler(ProviderSettingsSavedMsg{ProviderChanged: true, Provider: destination, Disabled: result.profile.Disabled})
}

func (m *providerDialogCmp) commitVaultMetadata(result providerVaultSaveMsg) (models.ModelProvider, error) {
	if m.legacy {
		return result.profile.Backend, result.prepared.CommitProvider(result.profile.Backend, result.profile.Provider)
	}
	return models.ModelProvider("profile:" + result.id), result.prepared.CommitProviderProfile(result.id, result.profile)
}
