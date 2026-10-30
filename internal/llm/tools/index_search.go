package tools

import (
	"context"
	"database/sql"
)

// Search state is captured under the index semaphore; ranking and network I/O
// consume this owned snapshot without blocking refresh/status in the workspace.
type indexSearchSnapshot struct {
	stats            indexRefresh
	paused, hybrid   bool
	candidates, pool []codeIndexHit
	strategy         string
}

func captureIndexSearch(ctx context.Context, db *sql.DB, root, scope string, input codeIndexInput) (indexSearchSnapshot, error) {
	s := indexSearchSnapshot{strategy: "lexical BM25 + exact identifiers"}
	var err error
	s.paused, err = indexPaused(root)
	if err != nil {
		return s, err
	}
	if !s.paused {
		s.stats, err = refreshCodeIndex(ctx, db, root)
		if err != nil {
			return s, err
		}
	}
	scoped := scope != "." || input.Glob != "" || input.Language != "" || input.Extension != ""
	if scoped {
		if err := createIndexScope(ctx, input, scope, db); err != nil {
			return s, err
		}
	}
	query, args, err := buildCodeIndexQuery(input, scoped)
	if err != nil {
		return s, err
	}
	s.candidates, err = readCodeIndexCandidates(ctx, db, query, args)
	if err != nil {
		return s, err
	}
	if input.Strategy == "hybrid" {
		s.captureSemanticPool(ctx, db, scoped, input.Kind)
	}
	return s, nil
}

func (s *indexSearchSnapshot) captureSemanticPool(ctx context.Context, db *sql.DB, scoped bool, kind string) {
	pool, err := embeddingCandidatePool(ctx, db, s.candidates, scoped, kind)
	if err != nil {
		s.strategy = "lexical fallback: semantic pool unavailable"
		return
	}
	s.hybrid, s.pool = true, pool
}
