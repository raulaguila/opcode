package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/raulaguila/opcode/internal/config"
)

// Cold queries request the query and at most fifteen source vectors together.
// Warm vectors do not consume this budget. The whole HTTP batch stays <=64 KiB.
func embeddingVectors(ctx context.Context, db *sql.DB, c config.EmbeddingConfig, texts []string) (map[string][]float64, error) {
	cache := embeddingCache{db: db, identity: c.Endpoint + "\x00" + c.Model + "\x00" + time.Now().UTC().Format("2006-01-02") + "\x00"}
	vectors := make(map[string][]float64, len(texts))
	missing, err := cache.missing(ctx, texts, vectors)
	if err != nil {
		return nil, err
	}
	if len(missing) == 0 {
		return vectors, nil
	}
	values, err := localEmbeddings(ctx, c, append([]string(nil), missing...))
	if err != nil {
		return nil, err
	}
	if err := cache.store(ctx, missing, values); err != nil {
		return nil, err
	}
	for i, text := range missing {
		vectors[text] = values[i]
	}
	return vectors, nil
}

type embeddingCache struct {
	db       *sql.DB
	identity string
}

func (c embeddingCache) key(text string) string { return indexDigest(c.identity + text) }

func (c embeddingCache) missing(ctx context.Context, texts []string, vectors map[string][]float64) ([]string, error) {
	statement, err := c.db.PrepareContext(ctx, `SELECT vector FROM embeddings WHERE key=?`)
	if err != nil {
		return nil, err
	}
	defer statement.Close()
	var missing []string
	seen := make(map[string]bool, len(texts))
	bytes := 0
	for _, text := range texts {
		if seen[text] {
			continue
		}
		seen[text] = true
		vector, err := cachedEmbedding(ctx, statement, c.key(text))
		if err != nil {
			return nil, err
		}
		if len(vector) > 0 {
			vectors[text] = vector
			continue
		}
		if len(missing) < 16 && bytes+len(text) <= 64<<10 {
			missing = append(missing, text)
			bytes += len(text)
		}
	}
	return missing, nil
}

func cachedEmbedding(ctx context.Context, statement *sql.Stmt, key string) ([]float64, error) {
	var raw string
	err := statement.QueryRowContext(ctx, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var vector []float64
	if json.Unmarshal([]byte(raw), &vector) != nil || len(vector) == 0 || len(vector) > 4096 {
		return nil, fmt.Errorf("invalid embedding cache")
	}
	return vector, nil
}

func (c embeddingCache) store(ctx context.Context, texts []string, values [][]float64) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `INSERT INTO embeddings(key,vector,at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET vector=excluded.vector,at=excluded.at`)
	if err != nil {
		return err
	}
	defer statement.Close()
	stamp := time.Now().Unix()
	for i, text := range texts {
		encoded, err := json.Marshal(values[i])
		if err != nil {
			return err
		}
		if _, err := statement.ExecContext(ctx, c.key(text), string(encoded), stamp); err != nil {
			return err
		}
	}
	if err := pruneEmbeddingCache(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func pruneEmbeddingCache(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM embeddings`).Scan(&count); err != nil {
		return err
	}
	if count <= 10000 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM embeddings WHERE key NOT IN (SELECT key FROM embeddings ORDER BY at DESC,key LIMIT 10000)`)
	return err
}
