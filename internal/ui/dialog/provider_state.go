package dialog

import (
	"strings"

	"github.com/raulaguila/opcode/internal/config"
)

// ProviderFormField is one visible row of the connection form.
type ProviderFormField struct {
	Label, Value, Placeholder string
	Options, OptionLabels     []string // choices drawn as radios; empty for text
	Focused, Secret           bool
}

// ProviderFormState is what the connection editor shows, for an interface
// that draws it in its own style while this editor keeps the keys,
// validation, credentials and the system vault.
type ProviderFormState struct {
	Title, Meta, Note string
	Warnings          []string
	Fields            []ProviderFormField
	Advanced          bool
	Testing, Saving   bool
	Tested            bool // the last catalog test succeeded
	CanEnable         bool // the connection is off: Ctrl+G saves and enables it
	Presets           []string
	Selected          int
}

// FormState describes the presets or the form; ok is false on other screens.
func (m *providerDialogCmp) FormState() (ProviderFormState, bool) {
	switch {
	case m.noteFull:
		return ProviderFormState{}, false
	case m.screen == "presets":
		state := ProviderFormState{Title: config.Text("Nova conexão", "New connection"), Selected: m.selected,
			Note: config.Text("Escolha um ponto de partida; revise antes de salvar.", "Choose a starting point; review before saving.")}
		for _, p := range config.ProviderPresets {
			state.Presets = append(state.Presets, interfaceCopy(p.Profile.Name))
		}
		return state, true
	case m.screen != "provider":
		return ProviderFormState{}, false
	}
	m.configureProfilePlaceholders()
	state := ProviderFormState{
		Title:    config.Text("Editar conexão", "Edit connection"),
		Meta:     m.fieldValue(providerNameField),
		Advanced: m.advanced, Testing: m.testing, Saving: m.saving,
		CanEnable: m.fields[providerActiveField].Value() == "false",
	}
	if m.originalID == "" {
		state.Title = config.Text("Nova conexão", "New connection")
	}
	for _, i := range m.fieldIndices() {
		field := ProviderFormField{
			Label:       interfaceCopy(fieldLabel(m.labels[i])),
			Value:       m.fields[i].Value(),
			Placeholder: m.fields[i].Placeholder,
			Options:     fieldOptions(m.labels[i]),
			Focused:     i == m.focus,
			Secret:      i == providerKeyField,
		}
		for _, option := range field.Options {
			field.OptionLabels = append(field.OptionLabels, radioLabel(option))
		}
		state.Fields = append(state.Fields, field)
	}
	state.Note = m.note
	switch {
	case m.saving:
		state.Note = config.Text("Salvando no cofre… aguarde o sistema; não é cancelável.", "Saving to the vault… wait for the system; cannot cancel.")
	case m.testing:
		state.Note = config.Text("Consultando o catálogo… Esc cancela.", "Querying the catalog… Esc cancels.")
	case state.Note == "" && m.fields[providerAuthField].Value() == "api_key":
		state.Note = config.Text("Ao salvar: chave no JSON local, sem criptografia.", "On save: key in local JSON, unencrypted.")
	case state.Note == "" && m.fields[providerAuthField].Value() == "vault":
		state.Note = config.Text("Chave no cofre do sistema; apenas referência no JSON.", "Key in the system vault; only a reference in JSON.")
	}
	state.Note = interfaceCopy(state.Note)
	state.Tested = m.catalogTested && !m.testing && !strings.HasSuffix(m.note, "/error")
	if m.originalID != "" && m.dirty() {
		state.Warnings = append(state.Warnings, config.Text("Alterações não salvas", "Unsaved changes"))
	}
	if m.fields[providerTLSField].Value() == "true" {
		state.Warnings = append(state.Warnings, config.Text("TLS sem validação: certificado e identidade não verificados.", "Insecure TLS: certificate and server identity are not verified."))
	}
	return state, true
}
