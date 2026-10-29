package tools

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
)

// Local embedding requests are explicit global configuration, loopback only,
// bounded, redacted, cancellable and never download model weights.
func localEmbeddings(ctx context.Context, c config.EmbeddingConfig, texts []string) ([][]float64, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if len(texts) < 1 || len(texts) > 16 {
		return nil, fmt.Errorf("embedding batch requires 1–16 inputs")
	}
	size := 0
	for i := range texts {
		texts[i] = RedactCredentials(texts[i])
		size += len(texts[i])
	}
	if size > 64<<10 {
		return nil, fmt.Errorf("embedding input exceeds 64 KiB")
	}
	raw, _ := json.Marshal(map[string]any{"model": c.Model, "input": texts, "truncate": false})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("local embedding service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local embedding service returned HTTP %d; lexical fallback used", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 2<<20 {
		return nil, fmt.Errorf("embedding response exceeds 2 MiB")
	}
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if json.Unmarshal(body, &out) != nil || len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("invalid embedding response")
	}
	dims := 0
	for _, v := range out.Embeddings {
		if dims == 0 {
			dims = len(v)
		}
		if dims < 1 || dims > 4096 || len(v) != dims {
			return nil, fmt.Errorf("embedding dimensions must match and be 1–4096")
		}
		for _, x := range v {
			if math.IsInf(x, 0) || math.IsNaN(x) {
				return nil, fmt.Errorf("non-finite embedding")
			}
		}
	}
	return out.Embeddings, nil
}
func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return -1
	}
	var dot, aa, bb float64
	for i := range a {
		dot += a[i] * b[i]
		aa += a[i] * a[i]
		bb += b[i] * b[i]
	}
	if aa == 0 || bb == 0 {
		return -1
	}
	return dot / math.Sqrt(aa*bb)
}

var errEmbeddingWarmBudget = errors.New("embedding cache warm budget reached")

func embeddingVector(ctx context.Context, db *sql.DB, c config.EmbeddingConfig, text string, budget *int) ([]float64, error) {
	key := indexDigest(c.Endpoint + "\x00" + c.Model + "\x00" + time.Now().UTC().Format("2006-01-02") + "\x00" + text)
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS embeddings(key TEXT PRIMARY KEY, vector TEXT NOT NULL,at INTEGER NOT NULL)`); err != nil {
		return nil, err
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT vector FROM embeddings WHERE key=?`, key).Scan(&raw)
	if err == nil {
		var v []float64
		if json.Unmarshal([]byte(raw), &v) == nil && len(v) > 0 && len(v) <= 4096 {
			return v, nil
		}
		return nil, fmt.Errorf("invalid embedding cache")
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if budget != nil {
		if *budget <= 0 {
			return nil, errEmbeddingWarmBudget
		}
		*budget--
	}
	values, err := localEmbeddings(ctx, c, []string{text})
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(values[0])
	if _, err := db.ExecContext(ctx, `INSERT INTO embeddings(key,vector,at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET vector=excluded.vector,at=excluded.at`, key, string(encoded), time.Now().Unix()); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM embeddings WHERE key NOT IN (SELECT key FROM embeddings ORDER BY at DESC,key LIMIT 10000)`); err != nil {
		return nil, err
	}
	return values[0], nil
}
func hybridCandidates(ctx context.Context, db *sql.DB, query string, candidates []codeIndexHit, scoped bool, kind string) ([]codeIndexHit, string) {
	cfg := config.Get()
	if cfg == nil || cfg.Index.Embeddings == nil {
		return candidates, "lexical (embeddings not configured)"
	}
	c := *cfg.Index.Embeddings
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	misses := 16
	// Include a bounded semantic pool so synonyms absent from lexical matches can
	// be recovered. Cache warming is deliberately incremental, at most 16 misses.
	q := `SELECT path,symbol,signature,kind,language,start_line,end_line,content,digest,0 FROM chunks WHERE 1=1`
	args := []any{}
	if scoped {
		q += ` AND path IN (SELECT path FROM scope_paths)`
	}
	if kind != "" {
		q += ` AND kind=?`
		args = append(args, kind)
	}
	q += ` ORDER BY (kind='text'),path,start_line LIMIT 128`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return candidates, "lexical fallback: semantic pool unavailable"
	}
	pool := append([]codeIndexHit(nil), candidates...)
	seen := map[string]bool{}
	for _, h := range pool {
		seen[fmt.Sprintf("%s:%d", h.Path, h.Start)] = true
	}
	for rows.Next() {
		var h codeIndexHit
		if rows.Scan(&h.Path, &h.Symbol, &h.Signature, &h.Kind, &h.Language, &h.Start, &h.End, &h.Preview, &h.Hash, &h.Rank) != nil {
			rows.Close()
			return candidates, "lexical fallback: semantic pool unavailable"
		}
		key := fmt.Sprintf("%s:%d", h.Path, h.Start)
		if !seen[key] {
			pool = append(pool, h)
			seen[key] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return candidates, "lexical fallback: semantic pool unavailable"
	}
	queryVector, err := embeddingVector(ctx, db, c, query, &misses)
	if err != nil {
		return candidates, "lexical fallback: local embeddings unavailable"
	}
	root, err := canonicalPath(config.WorkingDirectory())
	if err != nil {
		return candidates, "lexical fallback: current workspace unavailable"
	}
	verified := map[string]string{}
	scores := map[string]float64{}

	for i := range pool {
		if i >= 128 {
			break
		}
		h := pool[i]
		path := filepath.Join(root, filepath.FromSlash(h.Path))
		if !retrievalPathAllowed(root, path, nil) {
			continue
		}
		hash, ok := verified[path]
		if !ok {
			resolved, e := authorizeFile(ctx, path, "read")
			if e != nil || resolved != path {
				continue
			}
			raw, e := readFile(ctx, resolved)
			if e != nil {
				continue
			}
			hash = indexDigest(string(raw))
			verified[path] = hash
		}
		if hash != h.Hash {
			continue
		}
		text := compactPreview(h.Path+" "+h.Symbol+" "+h.Preview, 2000)
		v, err := embeddingVector(ctx, db, c, text, &misses)
		if errors.Is(err, errEmbeddingWarmBudget) {
			continue
		}
		if err != nil {
			return candidates, "lexical fallback: local embeddings unavailable"
		}
		key := fmt.Sprintf("%s:%d", h.Path, h.Start)
		scores[key] = cosine(queryVector, v)
	}
	indices := make([]int, 0, len(scores))
	for i, h := range pool {
		if _, ok := scores[fmt.Sprintf("%s:%d", h.Path, h.Start)]; ok {
			indices = append(indices, i)
		}
	}
	sort.SliceStable(indices, func(i, j int) bool {
		a, b := pool[indices[i]], pool[indices[j]]
		return scores[fmt.Sprintf("%s:%d", a.Path, a.Start)] > scores[fmt.Sprintf("%s:%d", b.Path, b.Start)]
	})
	fused := map[string]float64{}
	for i, h := range candidates {
		fused[fmt.Sprintf("%s:%d", h.Path, h.Start)] += 1 / float64(60+i+1)
	}
	for rank, index := range indices {
		h := pool[index]
		fused[fmt.Sprintf("%s:%d", h.Path, h.Start)] += 1 / float64(60+rank+1)
	}
	sort.SliceStable(pool, func(i, j int) bool {
		a, b := strings.EqualFold(pool[i].Symbol, strings.TrimSpace(query)), strings.EqualFold(pool[j].Symbol, strings.TrimSpace(query))
		if a != b {
			return a
		}
		return fused[fmt.Sprintf("%s:%d", pool[i].Path, pool[i].Start)] > fused[fmt.Sprintf("%s:%d", pool[j].Path, pool[j].Start)]
	})
	filtered := []codeIndexHit{}
	for _, h := range pool {
		key := fmt.Sprintf("%s:%d", h.Path, h.Start)
		if fused[key] > 0 {
			filtered = append(filtered, h)
		}
	}
	return filtered, "hybrid RRF; bounded semantic candidate pool (128), up to 16 vectors per request"
}
