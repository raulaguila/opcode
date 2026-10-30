//go:build integration

package evaluation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	appconfig "github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestLiveOllamaPagedIndexLookup(t *testing.T)              { testLiveOllamaIndexWorkflow(t, false) }
func TestLiveOllamaCompoundRetrievalRequirements(t *testing.T) { testLiveOllamaIndexWorkflow(t, true) }
func testLiveOllamaIndexWorkflow(t *testing.T, compound bool) {
	t.Helper()
	if appconfig.Environment("OPCODE_LIVE_TESTS") != "1" || appconfig.Environment("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("requires explicit local Ollama inference opt-in")
	}
	binary := appconfig.Environment("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	endpoint := os.Getenv("LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/v1"
	}
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, parsed.Hostname())
	model := appconfig.Environment("OPCODE_LIVE_OLLAMA_MODEL")
	if model == "" {
		model = "llama3.1:8b"
	}
	root := t.TempDir()
	home, workspace, state := filepath.Join(root, "home"), filepath.Join(root, "workspace"), filepath.Join(root, "state")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".opcode"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "src"), 0700))
	files := map[string]string{"src/service.py": "def calculate_invoice_total(amount):\n    return amount + 37\n", "src/service.ts": "export function calculate_invoice_total(amount: number) { return amount + 91; }\n"}
	for i := range 8 {
		files[fmt.Sprintf("file_%02d.txt", i)] = "placeholder\n"
	}
	for name, text := range files {
		require.NoError(t, os.WriteFile(filepath.Join(workspace, filepath.FromSlash(name)), []byte(text), 0600))
	}
	id := "local." + model
	cfg := map[string]any{"data": map[string]any{"directory": state}, "providers": map[string]any{"local": map[string]any{"baseURL": endpoint, "api": "ollama", "defaultMaxTokens": 2048}}, "models": map[string]any{id: map[string]any{"provider": "local", "api": "ollama", "api_model": model, "context_window": 16384}}, "agents": map[string]any{"coder": map[string]any{"model": id}}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".opcode", "config.json"), raw, 0600))
	prompts := []string{
		"Neste turno, use ls com path . e limit 2 para listar apenas os dois primeiros caminhos permitidos. Aguarde o resultado real e responda com essa página. Não execute comandos nem altere arquivos.",
		"Agora consulte a próxima página: use ls com path . e offset 2 e limit 2. Aguarde o resultado e responda com essa página. Não execute comandos nem altere arquivos.",
		"Use search_code_index com query calculate_invoice_total, path src e language python. Aguarde o resultado e informe o caminho Python encontrado. Não execute comandos nem altere arquivos.",
		"Leia src/service.py com view. Qual constante a função calculate_invoice_total adiciona ao valor? Responda com base nessa leitura real. Não execute comandos nem altere arquivos.",
	}
	if compound {
		prompts = []string{"Execute estes quatro passos de leitura e só então responda: 1. Use ls com path . offset 0 limit 2. 2. Use ls com path . offset 2 limit 2. 3. Use search_code_index com query calculate_invoice_total path src language python. 4. Use view com file_path src/service.py e informe a constante adicionada pela função. Aguarde os resultados reais em cada passo. Não execute comandos nem altere arquivos."}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	sessionID := ""
	var response struct{ Response string }
	for i, prompt := range prompts {
		args := []string{"--cwd", workspace, "--mode", "ask", "--prompt", prompt, "--quiet", "--output-format", "json", "--max-steps", "12", "--max-seconds", "220", "--first-token-timeout", "180"}
		if sessionID != "" {
			args = append(args, "--resume", sessionID)
		}
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "SHELL=/bin/bash", "LANG=C.UTF-8"}
		output, err := command.CombinedOutput()
		require.NoError(t, err, "real retrieval turn %d failed: %s", i+1, output)
		require.NoError(t, json.Unmarshal(output, &response))
		if sessionID == "" {
			conn, err := sql.Open("sqlite3", filepath.Join(state, "opcode.db"))
			require.NoError(t, err)
			err = conn.QueryRowContext(ctx, `SELECT id FROM sessions WHERE parent_session_id IS NULL ORDER BY created_at DESC LIMIT 1`).Scan(&sessionID)
			require.NoError(t, err)
			require.NoError(t, conn.Close())
		}
	}
	require.Contains(t, response.Response, "37")
	calls, results := liveRetrievalReceipts(t, ctx, state)
	assertLiveRetrievalReceipts(t, calls, results)
	for name, text := range files {
		actual, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(name)))
		require.NoError(t, err)
		require.Equal(t, text, string(actual))
	}
	t.Logf("Real Ollama retrieval workflow: compound=%t, all four required operations verified at 16k; files unchanged", compound)
}

func liveRetrievalReceipts(t *testing.T, ctx context.Context, state string) (map[string]message.ToolCall, map[string]message.ToolResult) {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(state, "opcode.db"))
	require.NoError(t, err)
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT parts FROM messages`)
	require.NoError(t, err)
	defer rows.Close()
	calls := map[string]message.ToolCall{}
	results := map[string]message.ToolResult{}
	for rows.Next() {
		var parts string
		require.NoError(t, rows.Scan(&parts))
		var wrappers []struct {
			Type string
			Data json.RawMessage
		}
		require.NoError(t, json.Unmarshal([]byte(parts), &wrappers))
		for _, part := range wrappers {
			switch part.Type {
			case "tool_call":
				var call message.ToolCall
				require.NoError(t, json.Unmarshal(part.Data, &call))
				calls[call.ID] = call
			case "tool_result":
				var result message.ToolResult
				require.NoError(t, json.Unmarshal(part.Data, &result))
				results[result.ToolCallID] = result
			}
		}
	}
	require.NoError(t, rows.Err())
	return calls, results
}

type retrievalReceiptCoverage struct{ first, next, index, read bool }

func (c *retrievalReceiptCoverage) observe(t *testing.T, call message.ToolCall, result message.ToolResult) {
	t.Helper()
	var args struct {
		Path, Language string
		Offset, Limit  int
	}
	require.NoError(t, json.Unmarshal([]byte(call.Input), &args))
	t.Logf("Successful tool: %s path=%q language=%q offset=%d limit=%d", call.Name, args.Path, args.Language, args.Offset, args.Limit)
	if call.Name == "ls" && args.Limit == 2 {
		c.first = c.first || args.Offset == 0
		c.next = c.next || args.Offset == 2
		require.Contains(t, result.Content, "next_offset")
	}
	if call.Name == "search_code_index" && args.Language == "python" {
		c.index = true
		require.Contains(t, result.Content, "src/service.py")
		require.NotContains(t, result.Content, "src/service.ts")
	}
	var view struct {
		Path string `json:"file_path"`
	}
	require.NoError(t, json.Unmarshal([]byte(call.Input), &view))
	if call.Name == "view" && strings.HasSuffix(view.Path, "service.py") {
		c.read = true
	}
}

func assertLiveRetrievalReceipts(t *testing.T, calls map[string]message.ToolCall, results map[string]message.ToolResult) {
	t.Helper()
	coverage := retrievalReceiptCoverage{}
	for id, call := range calls {
		result, ok := results[id]
		if !ok || result.IsError {
			continue
		}
		coverage.observe(t, call, result)
	}
	require.True(t, coverage.first, "model must request the first bounded file page")
	require.True(t, coverage.next, "model must request the next page")
	require.True(t, coverage.index, "model must use the filtered index")
	require.True(t, coverage.read, "model must read current source after the preview")
}
