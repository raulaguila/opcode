package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestProjectOverviewReadsImplementationBeforeFirstModelRequest(t *testing.T) {
	for _, path := range []string{"service.go", "service.py", "service.ts"} {
		t.Run(path, func(t *testing.T) {
			a, p, _, root := runtimeAgent(t, workflow.Ask)
			require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# River\nThe documentation describes a dashboard.\n"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("// Application service handler\n// VERIFIED_IMPLEMENTATION_FACT: receives sensor samples\n"), 0600))
			read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
			a.tools = []tools.BaseTool{read, tools.NewCodeIndexTool()}
			done, err := a.Run(context.Background(), "s", "o que pode me falar sobre esse projeto?")
			require.NoError(t, err)
			call := nextGeneration(t, p)
			raw, err := json.Marshal(call.history)
			require.NoError(t, err)
			require.Contains(t, string(raw), "VERIFIED_IMPLEMENTATION_FACT")
			require.Contains(t, toolNames(call), "view")
			require.Contains(t, toolNames(call), "search_code_index")
			require.LessOrEqual(t, read.count.Load(), int32(3), "overview preparation must stay bounded")
			call.reply <- provider.ProviderResponse{Content: "A documentação descreve um dashboard (README.md:2); a implementação recebe amostras de sensores (" + path + ":2).", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
		})
	}
}

func TestOverviewEntryCallsSkipLoggingWithoutLosingTheApplicationEntry(t *testing.T) {
	content := "1|func main() {\n2|defer logging.RecoverPanic(\"main\", func() {\n3|logging.ErrorPersist(\"failure\")\n4|})\n5|cmd.Execute()\n6|}\n"
	require.Equal(t, []string{"Execute"}, overviewEntryCalls(content))
}

func TestOverviewKeepsManifestHeadersUnlessDependenciesWereRequested(t *testing.T) {
	require.Equal(t, 6, overviewSourceLimit("go.mod", "o que pode falar sobre esse projeto?"))
	require.Equal(t, 80, overviewSourceLimit("go.mod", "fale sobre o projeto e suas dependências"))
	require.Equal(t, 80, overviewSourceLimit("README.md", "o que pode falar sobre esse projeto?"))
}

func TestOverviewResponseGuidanceSurvivesCitationRepairWithoutApplyingToSpecificQuestions(t *testing.T) {
	run := &generationRun{turnMessageID: "user"}
	history := []message.Message{{ID: "user", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "o que pode me falar sobre esse projeto?"}}}, {ID: "repair", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Application validation: fix the source reference", Origin: "application"}}}}
	guidance := overviewResponseGuidance(run, history)
	require.Contains(t, guidance, "practical capabilities")
	require.Contains(t, guidance, "when repairing citations")
	require.Contains(t, guidance, "one explicit sentence")
	require.Empty(t, overviewResponseGuidance(&generationRun{turnMessageID: "repair"}, history))
}

func TestOverviewFollowsEntryCallsInsteadOfItsOwnSearchLiteral(t *testing.T) {
	for _, language := range []struct{ entry, source, program, implementation string }{
		{"main.go", "service.go", "package main\nfunc main() { collectSamples() }\n", "package main\nfunc collectSamples() string { return \"VERIFIED_SENSOR_PIPELINE\" }\n"},
		{"main.py", "service.py", "from service import collect_samples\ncollect_samples()\n", "def collect_samples():\n    return 'VERIFIED_SENSOR_PIPELINE'\n"},
		{"src/main.ts", "service.ts", "import { collectSamples } from '../service';\ncollectSamples();\n", "export function collectSamples() {\n return 'VERIFIED_SENSOR_PIPELINE';\n}\n"},
	} {
		t.Run(language.entry, func(t *testing.T) {
			a, p, _, root := runtimeAgent(t, workflow.Ask)
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, language.entry)), 0700))
			for path, content := range map[string]string{"README.md": "# Sensor project\nCollects samples.\n", language.entry: language.program, language.source: language.implementation, "context_bootstrap.go": "package main\nfunc bootstrap() string { return \"application service handler run execute DECOY_QUERY_LITERAL\" }\n"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0600))
			}
			read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
			a.tools = []tools.BaseTool{read, tools.NewCodeIndexTool()}
			done, err := a.Run(context.Background(), "s", "o que pode me falar sobre esse projeto?")
			require.NoError(t, err)
			call := nextGeneration(t, p)
			raw, err := json.Marshal(call.history)
			require.NoError(t, err)
			require.Contains(t, string(raw), "VERIFIED_SENSOR_PIPELINE")
			require.NotContains(t, string(raw), "DECOY_QUERY_LITERAL")
			require.LessOrEqual(t, read.count.Load(), int32(4))
			call.reply <- provider.ProviderResponse{Content: "O projeto coleta amostras (" + language.source + ":1-2).", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
		})
	}
}
