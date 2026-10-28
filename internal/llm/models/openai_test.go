package models

import "testing"

func TestGPT41MiniAPIModel(t *testing.T) {
	if got := SupportedModels[GPT41Mini].APIModel; got != "gpt-4.1-mini" {
		t.Fatalf("GPT-4.1 Mini sends requests to %q", got)
	}
}
