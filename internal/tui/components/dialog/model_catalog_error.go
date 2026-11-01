package dialog

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/util"
)

// Only fixed application copy reaches the view and diagnostic channel.
func catalogFailureHint(err error) string {
	var catalog *models.CatalogError
	var certificate *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCert x509.CertificateInvalidError
	var recordHeader tls.RecordHeaderError
	var alert tls.AlertError
	var network net.Error
	text := strings.ToLower(err.Error())
	switch {
	case errors.As(err, &catalog) && (catalog.StatusCode == 401 || catalog.StatusCode == 403),
		strings.Contains(text, "credential vault"), strings.Contains(text, "environment variable") && strings.Contains(text, "not set"):
		return config.Text("Autenticação: reveja chave e acesso", "Authentication: check key and access")
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		return config.Text("Tempo esgotado: tente ou confira o servidor", "Timeout: retry or check server")
	case errors.As(err, &certificate), errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalidCert), errors.As(err, &recordHeader), errors.As(err, &alert):
		return config.Text("TLS: confira certificado e confiança", "TLS: check certificate and trust")
	case strings.Contains(text, "invalid model catalog response"), strings.Contains(text, "invalid model list"),
		strings.Contains(text, "response exceeds limit"), strings.Contains(text, "cannot read local model list"):
		return config.Text("Formato: confira catálogo e API da conexão", "Format: check catalog and connection API")
	case catalog != nil && catalog.StatusCode > 0:
		return fmt.Sprintf(config.Text("Catálogo HTTP %d: confira o servidor", "Catalog HTTP %d: check server"), catalog.StatusCode)
	case errors.As(err, &network), strings.Contains(text, "cannot connect"), strings.Contains(text, "cannot reach"):
		return config.Text("Rede: confira servidor e conexão", "Network: check server and connection")
	default:
		return config.Text("Falha no catálogo: reveja a conexão", "Catalog query failed: check connection")
	}
}

func catalogFailureNotice(summary string) util.InfoMsg {
	return util.InfoMsg{Type: util.InfoTypeError, Msg: summary, Details: summary + "\n" + config.Text("Ctrl+R tenta novamente; Ctrl+P revisa a conexão em Modelos. Nenhum modelo foi selecionado.", "Ctrl+R retries; Ctrl+P reviews the connection in Models. No model was selected.")}
}
