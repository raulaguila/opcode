package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSQLCLifecyclePreservesEveryStatementAndIsIdempotent(t *testing.T) {
	raw := []byte(`package db
import "database/sql"
func Prepare(ctx context.Context, db DBTX) (*Queries,error) {
 q:=Queries{db:db}
 var err error
 if q.firstStmt,err=db.PrepareContext(ctx,first);err!=nil {return nil,fmt.Errorf("first: %w",err)}
 if q.lastStmt,err=db.PrepareContext(ctx,last);err!=nil {return nil,fmt.Errorf("last: %w",err)}
 return &q,nil
}
func(q *Queries) Close() error {return nil}
`)
	normalized, err := normalize(raw)
	require.NoError(t, err)
	require.Contains(t, string(normalized), `{"First", "firstStmt", &q.firstStmt, first}`)
	require.Contains(t, string(normalized), `{"Last", "lastStmt", &q.lastStmt, last}`)
	require.Less(t, bytes.Index(normalized, []byte(`&q.firstStmt`)), bytes.Index(normalized, []byte(`&q.lastStmt`)), "preparation and final close-error order must survive regeneration")
	again, err := normalize(normalized)
	require.NoError(t, err)
	require.Equal(t, normalized, again)
}

func TestNormalizeRejectsUnrecognizedSQLCShape(t *testing.T) {
	for _, raw := range []string{
		`package db; func Prepare(){ }; func Close(){ }`,
		`package db; func Prepare(){if err := other(); err!=nil{return}}; func Close(){}`,
		`package db; func Prepare(){if first,err=prepare(other);err!=nil{return}}; func Close(){}`,
	} {
		_, err := normalize([]byte(raw))
		require.Error(t, err, "an unsupported generator output must not silently drop statements")
	}
}
