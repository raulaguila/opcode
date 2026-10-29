package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/spf13/viper"
)

// Ranking evaluation calls the production search tool, not an agent or fake.
// Its fixture schema is separate so the original workflow gate remains intact.
type RetrievalSuite struct {
	Version int             `json:"version"`
	Cases   []RetrievalCase `json:"cases"`
}
type RetrievalCase struct {
	ID       string            `json:"id"`
	Files    map[string]string `json:"files"`
	Query    string            `json:"query"`
	Path     string            `json:"path,omitempty"`
	Language string            `json:"language,omitempty"`
	Expected []string          `json:"expected"`
}
type RankingResult struct {
	ID                string   `json:"id"`
	RequestedStrategy string   `json:"requested_strategy"`
	EffectiveStrategy string   `json:"effective_strategy"`
	Cache             string   `json:"cache"`
	Status            string   `json:"status"`
	Recall            *float64 `json:"recall"`
	MRR               *float64 `json:"mrr"`
	ResultCount       int      `json:"result_count"`
	ResponseBytes     int      `json:"response_bytes"`
	DurationMS        int64    `json:"duration_ms"`
	ScanReads         int      `json:"scan_reads"`
	ReusedFiles       int      `json:"reused_files"`
	UpdatedFiles      int      `json:"updated_files"`
	Error             string   `json:"error,omitempty"`
}
type RankingReport struct {
	Version              int             `json:"version"`
	Mode                 string          `json:"mode"`
	SuiteSHA256          string          `json:"suite_sha256"`
	EmbeddingsConfigured bool            `json:"embeddings_configured"`
	Results              []RankingResult `json:"results"`
	Notice               string          `json:"notice"`
}
type rankingJob struct {
	Workspace string        `json:"workspace"`
	Config    string        `json:"config"`
	Case      RetrievalCase `json:"case"`
	Strategy  string        `json:"strategy"`
}

func (s RetrievalSuite) Validate() error {
	if s.Version != 1 || len(s.Cases) < 1 || len(s.Cases) > 16 {
		return fmt.Errorf("ranking suite needs version 1 and 1–16 cases")
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if seen[c.ID] || strings.TrimSpace(c.Query) == "" || len(c.Query) > 512 || len(c.Expected) < 1 || len(c.Expected) > 16 || c.Path != "" && c.Path != "." && !safePath(c.Path) || len(c.Language) > 80 || strings.ContainsAny(c.Language, "/\\\r\n\x00") {
			return fmt.Errorf("invalid ranking fixture/query/scope")
		}
		seen[c.ID] = true
		stub := Case{ID: c.ID, Mode: "ask", Files: c.Files, Prompts: []string{"ranking fixture"}, Criteria: []Criterion{{Kind: "response_contains", Value: "ranking"}}, ExpectedRetrieval: c.Expected}
		if err := (Suite{Version: 1, Cases: []Case{stub}}).Validate(); err != nil {
			return err
		}
		for _, path := range c.Expected {
			if _, ok := c.Files[path]; !ok {
				return fmt.Errorf("expected ranking path must exist in fixture")
			}
		}
	}
	return nil
}

func LoadRetrieval(path string) (RetrievalSuite, []byte, error) {
	var raw []byte
	var err error
	if path == "" {
		raw, err = fixtures.ReadFile("testdata/retrieval.json")
	} else {
		raw, err = readBounded(path, 2<<20)
	}
	if err != nil {
		return RetrievalSuite{}, nil, err
	}
	var suite RetrievalSuite
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err = d.Decode(&suite); err != nil {
		return suite, nil, fmt.Errorf("invalid ranking suite JSON")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return suite, nil, fmt.Errorf("ranking suite requires one JSON document")
	}
	return suite, raw, suite.Validate()
}

func LoadEmbeddingConfig(path string) (*config.EmbeddingConfig, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := readBounded(path, 32<<10)
	if err != nil {
		return nil, fmt.Errorf("cannot read embedding configuration")
	}
	var c config.EmbeddingConfig
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return nil, fmt.Errorf("embedding JSON only accepts endpoint and model")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("embedding JSON requires one document")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func RunRetrieval(ctx context.Context, suite RetrievalSuite, raw []byte, executable string, embeddings *config.EmbeddingConfig) (RankingReport, error) {
	hash := sha256.Sum256(raw)
	report := RankingReport{Version: 1, Mode: "direct_tool_ranking", SuiteSHA256: hex.EncodeToString(hash[:]), EmbeddingsConfigured: embeddings != nil, Notice: "Production search tool only: no chat generation. Each case/strategy starts in a fresh disposable workspace, then repeats the same query on its warm index. Hybrid may fall back to lexical; the effective strategy is recorded. Configured embeddings make bounded local HTTP calls and never download models. Ranking metrics do not prove answer quality."}
	if err := suite.Validate(); err != nil {
		return report, err
	}
	if executable == "" {
		return report, fmt.Errorf("ranking evaluation requires the Opcode executable")
	}
	if embeddings != nil {
		if err := embeddings.Validate(); err != nil {
			return report, err
		}
	}
	for _, c := range suite.Cases {
		for _, strategy := range []string{"lexical", "hybrid"} {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			base, root, err := Prepare(Case{Files: c.Files})
			if err != nil {
				return report, err
			}
			results, runErr := runRankingCase(ctx, base, root, c, strategy, executable, embeddings)
			removeErr := os.RemoveAll(base)
			if runErr != nil {
				return report, runErr
			}
			if removeErr != nil {
				return report, removeErr
			}
			report.Results = append(report.Results, results...)
		}
	}
	return report, nil
}

func runRankingCase(ctx context.Context, base, root string, c RetrievalCase, strategy, executable string, embeddings *config.EmbeddingConfig) ([]RankingResult, error) {
	configPath := filepath.Join(base, "config.json")
	if err := localstate.Write(configPath, map[string]any{"data": map[string]string{"directory": filepath.Join(base, "state")}, "security": map[string]string{"sandbox": "bubblewrap"}, "index": config.IndexConfig{Embeddings: embeddings}}, 2<<20); err != nil {
		return nil, err
	}
	jobPath := filepath.Join(base, "ranking-job.json")
	if err := localstate.Write(jobPath, rankingJob{Workspace: root, Config: configPath, Case: c, Strategy: strategy}, 2<<20); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "evaluate-retrieval-worker", "--job", jobPath)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "XDG_CONFIG_HOME=" + filepath.Join(base, "config"), "SHELL=/bin/bash", "LANG=C.UTF-8"}
	var output boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return []RankingResult{{ID: c.ID, RequestedStrategy: strategy, Cache: "cold", Status: "failed", Error: "ranking worker failed or timed out; payload omitted"}}, nil
	}
	var results []RankingResult
	if output.overflow || json.Unmarshal(output.data, &results) != nil || len(results) != 2 {
		return nil, fmt.Errorf("invalid bounded ranking worker report")
	}
	return results, nil
}

func RetrievalWorker(ctx context.Context, jobPath string, out io.Writer) error {
	raw, err := readBounded(jobPath, 2<<20)
	if err != nil {
		return err
	}
	var job rankingJob
	if json.Unmarshal(raw, &job) != nil {
		return fmt.Errorf("invalid ranking job")
	}
	base := filepath.Dir(jobPath)
	if !strings.HasPrefix(filepath.Base(base), "opcode-eval-") || job.Workspace != filepath.Join(base, "workspace") || job.Config != filepath.Join(base, "config.json") || job.Strategy != "lexical" && job.Strategy != "hybrid" {
		return fmt.Errorf("ranking worker requires disposable paths and known strategy")
	}
	if err := (RetrievalSuite{Version: 1, Cases: []RetrievalCase{job.Case}}).Validate(); err != nil {
		return err
	}
	if config.Get() != nil {
		return fmt.Errorf("ranking worker requires fresh configuration")
	}
	if err := os.Chdir(job.Workspace); err != nil {
		return err
	}
	viper.SetConfigFile(job.Config)
	if _, err := config.LoadOffline(job.Workspace); err != nil {
		return fmt.Errorf("ranking configuration failed")
	}
	query, _ := json.Marshal(map[string]any{"query": job.Case.Query, "path": job.Case.Path, "language": job.Case.Language, "strategy": job.Strategy, "intent": "implementation", "limit": 12})
	tool := tools.SecureTool(tools.NewCodeIndexTool(), permission.NewPermissionService())
	results := []RankingResult{}
	for _, cache := range []string{"cold", "warm"} {
		started := time.Now()
		response, err := tool.Run(ctx, tools.ToolCall{ID: "ranking-" + cache, Name: tool.Info().Name, Input: string(query)})
		r := RankingResult{ID: job.Case.ID, RequestedStrategy: job.Strategy, Cache: cache, Status: "measured", DurationMS: time.Since(started).Milliseconds(), ResponseBytes: len(response.Content)}
		var page struct {
			Results      []struct{ Path string }
			Strategy     string
			ScanReads    int `json:"scan_reads"`
			ReusedFiles  int `json:"reused_files"`
			UpdatedFiles int `json:"updated_files"`
		}
		if err != nil || response.IsError || json.Unmarshal([]byte(response.Content), &page) != nil {
			r.Status = "failed"
			r.Error = "search tool failed; payload omitted"
		} else {
			paths := []string{}
			for _, hit := range page.Results {
				paths = append(paths, hit.Path)
			}
			recall, mrr := retrievalQuality(job.Case.Expected, paths)
			r.Recall = &recall
			r.MRR = &mrr
			r.EffectiveStrategy = page.Strategy
			r.ResultCount = len(paths)
			r.ScanReads = page.ScanReads
			r.ReusedFiles = page.ReusedFiles
			r.UpdatedFiles = page.UpdatedFiles
		}
		results = append(results, r)
	}
	return json.NewEncoder(out).Encode(results)
}
