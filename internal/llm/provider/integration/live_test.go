//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	appconfig "github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
)

func TestLiveProviders(t *testing.T) {
	// Both the build tag and this explicit flag are required. API keys alone
	// never enable billable tests, and ordinary CI does not compile this entry.
	if appconfig.Environment("OPCODE_LIVE_TESTS") != "1" {
		t.Skip("opt in with OPCODE_LIVE_TESTS=1 and -tags=integration")
	}
	path, required, err := liveConfigPath(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := readLiveConfig(path, required)
	if err != nil {
		t.Fatal(err)
	}
	cases, timeout, err := resolveLiveCases(cfg, os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.spec.name, func(t *testing.T) {
			for name, value := range test.environment {
				t.Setenv(name, value)
			}
			c := test.config
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()
			// Do not expose values through panic diagnostics from credential discovery.
			defer func() {
				if recover() != nil {
					t.Fatal("provider panicked; details suppressed to protect credentials")
				}
			}()
			client, err := provider.NewProvider(test.spec.provider,
				provider.WithAPIKey(c.APIKey),
				provider.WithModel(models.Model{ID: models.ModelID(test.spec.name + "-integration"), Provider: test.spec.provider, APIModel: c.Model, API: test.spec.api, CanReason: c.CanReason, ThinkingMode: c.ThinkingMode}),
				provider.WithMaxTokens(c.MaxTokens),
				provider.WithSystemMessage("You are running a synthetic API integration test. Follow the requested output format. Only the supplied harmless probe tool is available. Never request external commands, files or secrets."),
				provider.WithOpenAIOptions(provider.WithReasoningEffort(c.ReasoningEffort)),
				provider.WithCopilotOptions(provider.WithCopilotReasoningEffort(c.ReasoningEffort)),
			)
			if err != nil {
				t.Fatal("provider initialization failed:", safeLiveError(err))
			}
			runLiveChecks(t, ctx, client, c.Tools == nil || *c.Tools)
		})
	}
}
