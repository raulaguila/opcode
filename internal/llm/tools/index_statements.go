package tools

import (
	"context"
	"database/sql"
	"errors"
)

// Statements belong to one refresh transaction, never to a global DB handle.
type indexStatements struct {
	previous, seen, unsee, upsert, deleteChunks, insertChunk *sql.Stmt
}

func prepareIndexStatements(ctx context.Context, tx *sql.Tx) (*indexStatements, error) {
	s := &indexStatements{}
	queries := []struct {
		destination **sql.Stmt
		query       string
	}{
		{&s.previous, `SELECT digest,stamp,verified,policy FROM files WHERE path=?`},
		{&s.seen, `INSERT INTO seen(path) VALUES(?)`},
		{&s.unsee, `DELETE FROM seen WHERE path=?`},
		{&s.upsert, `INSERT INTO files(path,digest,stamp,verified,policy) VALUES(?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET digest=excluded.digest,stamp=excluded.stamp,verified=excluded.verified,policy=excluded.policy`},
		{&s.deleteChunks, `DELETE FROM chunks WHERE path=?`},
		{&s.insertChunk, `INSERT INTO chunks(path,symbol,signature,content,terms,kind,language,start_line,end_line,digest) VALUES(?,?,?,?,?,?,?,?,?,?)`},
	}
	for _, query := range queries {
		statement, err := tx.PrepareContext(ctx, query.query)
		if err != nil {
			_ = s.Close()
			return nil, err
		}
		*query.destination = statement
	}
	return s, nil
}

func (s *indexStatements) Close() error {
	var result error
	for _, statement := range []*sql.Stmt{s.previous, s.seen, s.unsee, s.upsert, s.deleteChunks, s.insertChunk} {
		if statement != nil {
			result = errors.Join(result, statement.Close())
		}
	}
	return result
}
