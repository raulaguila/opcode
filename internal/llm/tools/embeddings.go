package tools

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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
	redactor := newCredentialRedactor(credentialValues())
	for i := range texts {
		texts[i] = redactor.Redact(texts[i])
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
	return decodeLocalEmbeddings(body, len(texts))
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

// The candidate pool is captured under the workspace index lock. HTTP and the
// embedding cache use this request's DB handle after that lock is released.
func rankHybridCandidates(ctx context.Context, db *sql.DB, query string, candidates, pool []codeIndexHit, root string) ([]codeIndexHit, string) {
	cfg := config.Get()
	if cfg == nil || cfg.Index.Embeddings == nil {
		return candidates, "lexical (embeddings not configured)"
	}
	c := *cfg.Index.Embeddings
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	texts, eligible := semanticInputs(ctx, query, pool, root)
	vectors, err := embeddingVectors(ctx, db, c, texts)
	if err != nil || len(vectors[query]) == 0 {
		return candidates, "lexical fallback: local embeddings unavailable"
	}
	scores := make(map[string]float64, len(eligible))
	for _, candidate := range eligible {
		if vector := vectors[candidate.text]; len(vector) > 0 {
			scores[candidate.key] = cosine(vectors[query], vector)
		}
	}
	return fuseSemanticCandidates(query, candidates, pool, scores), "hybrid RRF; bounded semantic candidate pool (128), up to 16 vectors per request"
}

type semanticInput struct{ key, text string }

func semanticInputs(ctx context.Context, query string, pool []codeIndexHit, root string) ([]string, []semanticInput) {
	verified := map[string]string{}
	texts := []string{query}
	eligible := make([]semanticInput, 0, min(128, len(pool)))
	for _, hit := range pool[:min(128, len(pool))] {
		hash, ok := verifiedSemanticHash(ctx, root, hit.Path, verified)
		if !ok || hash != hit.Hash {
			continue
		}
		text := compactPreview(hit.Path+" "+hit.Symbol+" "+hit.Preview, 2000)
		texts = append(texts, text)
		eligible = append(eligible, semanticInput{key: fmt.Sprintf("%s:%d", hit.Path, hit.Start), text: text})
	}
	return texts, eligible
}

func decodeLocalEmbeddings(body []byte, count int) ([][]float64, error) {
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if json.Unmarshal(body, &out) != nil || len(out.Embeddings) != count {
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

func embeddingCandidatePool(ctx context.Context, db *sql.DB, candidates []codeIndexHit, scoped bool, kind string) ([]codeIndexHit, error) {
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
		return nil, err
	}
	pool := append([]codeIndexHit(nil), candidates...)
	seen := map[string]bool{}
	for _, h := range pool {
		seen[fmt.Sprintf("%s:%d", h.Path, h.Start)] = true
	}
	for rows.Next() {
		var h codeIndexHit
		if scanErr := rows.Scan(&h.Path, &h.Symbol, &h.Signature, &h.Kind, &h.Language, &h.Start, &h.End, &h.Preview, &h.Hash, &h.Rank); scanErr != nil {
			rows.Close()
			return nil, scanErr
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
		return nil, err
	}

	return pool, nil
}

func verifiedSemanticHash(ctx context.Context, root, relative string, verified map[string]string) (string, bool) {
	path := filepath.Join(root, filepath.FromSlash(relative))
	if !retrievalPathAllowed(root, path, nil) {
		return "", false
	}
	hash, ok := verified[path]
	if !ok {
		resolved, e := authorizeFile(ctx, path, "read")
		if e != nil || resolved != path {
			return "", false
		}
		raw, e := readFile(ctx, resolved)
		if e != nil {
			return "", false
		}
		hash = indexDigest(string(raw))
		verified[path] = hash
	}

	return hash, true
}

func fuseSemanticCandidates(query string, candidates, pool []codeIndexHit, scores map[string]float64) []codeIndexHit {
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
	return filtered
}
