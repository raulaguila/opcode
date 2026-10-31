package dialog

import (
	"errors"
	"strconv"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
)

func (m *providerDialogCmp) fieldValue(index int) string {
	return strings.TrimSpace(m.fields[index].Value())
}

func (m *providerDialogCmp) validateProfileIdentity(id string, profile config.ProviderProfile) error {
	if m.originalID != "" && id != m.originalID {
		return errors.New(config.Text("O ID da conexão não pode ser alterado; adicione outra conexão", "profile ID cannot be changed; add a new connection instead"))
	}
	if m.legacy && profile.Backend != models.ModelProvider(id) {
		return errors.New(config.Text("O provedor legado não pode ser alterado; adicione uma conexão nomeada", "legacy backend cannot be changed; add a named profile instead"))
	}
	return nil
}

func (m *providerDialogCmp) readProfileLimits(profile config.ProviderProfile) (config.ProviderProfile, error) {
	var err error
	profile.ContextWindow, err = strconv.ParseInt(m.fieldValue(providerContextField), 10, 64)
	if err != nil || profile.ContextWindow < 0 {
		return profile, errors.New(config.Text("A janela de contexto deve ser um inteiro não negativo", "context window must be a nonnegative integer"))
	}
	profile.DefaultMaxTokens, err = strconv.ParseInt(m.fieldValue(providerOutputField), 10, 64)
	if err != nil || profile.DefaultMaxTokens < 0 {
		return profile, errors.New(config.Text("Os tokens de saída devem ser um inteiro não negativo", "output tokens must be a nonnegative integer"))
	}
	active, err := strconv.ParseBool(m.fieldValue(providerActiveField))
	profile.Disabled = !active
	if err != nil {
		return profile, errors.New(config.Text("Conexão ativa deve ser true ou false", "connection active must be true or false"))
	}
	profile.TLSInsecure, err = strconv.ParseBool(m.fieldValue(providerTLSField))
	if err != nil {
		return profile, errors.New(config.Text("TLS inseguro deve ser true ou false", "TLS insecure must be true or false"))
	}
	return profile, nil
}

func (m *providerDialogCmp) readProfileAuthentication(profile config.ProviderProfile) (config.ProviderProfile, error) {
	switch m.fieldValue(providerAuthField) {
	case "api_key":
		profile.APIKey = m.fieldValue(providerKeyField)
		if profile.APIKey == "" && !profile.Disabled {
			return profile, errors.New(config.Text("Informe a chave da API ou escolha outra autenticação", "Enter the API key or choose another authentication method"))
		}
	case "vault":
		profile.APIKeyRef = m.originalKeyRef
		if profile.APIKeyRef == "" && m.fieldValue(providerKeyField) == "" {
			return profile, errors.New(config.Text("Informe a chave para o cofre nativo", "Enter the API key for the native vault"))
		}
	case "environment":
		profile.APIKeyEnv = m.fieldValue(providerEnvironmentField)
		if profile.APIKeyEnv == "" && !profile.Disabled {
			return profile, errors.New(config.Text("Informe o nome da variável de ambiente", "Enter the environment variable name"))
		}
	case "none":
	default:
		return profile, errors.New(config.Text("Método de autenticação inválido", "invalid authentication method"))
	}
	return profile, nil
}

func (m *providerDialogCmp) configureProfilePlaceholders() {
	if m.screen != "provider" {
		return
	}
	m.fields[providerURLField].Placeholder = config.Text("Padrão do provedor", "Provider default")
	m.fields[providerEnvironmentField].Placeholder = config.Text("Nome da variável com a chave", "Variable holding the API key")
	if m.fields[providerBackendField].Value() == "local" {
		m.fields[providerURLField].Placeholder = "https://server.example/v1"
	}
	m.fields[providerKeyField].Placeholder = config.Text("Cole sua chave da API", "Paste your API key")
	if m.fields[providerAuthField].Value() == "vault" && m.originalKeyRef != "" {
		m.fields[providerKeyField].Placeholder = config.Text("Vazio mantém a chave no cofre", "Blank keeps the existing vault key")
	}
}
