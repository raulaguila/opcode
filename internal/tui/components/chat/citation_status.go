package chat

import (
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
)

func citationStatusLabel(state string) string {
	switch state {
	case message.CitationPending:
		return config.Text("Validando citações…", "Validating citations…")
	case message.CitationInvalid:
		return config.Text("Citações não validadas", "Citations not validated")
	case message.CitationSuperseded:
		return config.Text("Resposta substituída", "Response superseded")
	default:
		return ""
	}
}
