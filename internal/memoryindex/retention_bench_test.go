package memoryindex

import (
	"context"
	"fmt"
	"testing"
)

// This paired fixture isolates document-retention overhead: metadata has 100 or
// 10000 rows and only the hot document has FTS text. Upsert includes opening the
// database, schema checks and the transaction, as in the original audit fixture.
func BenchmarkMemoryProjection(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) { benchmarkMemoryProjection(b, count) })
	}
}
func benchmarkMemoryProjection(b *testing.B, count int) {
	benchmarkMemoryProjectionFixture(b, count, false)
}

// Include populated FTS rows so a retention change cannot hide an unnecessary
// full-text scan behind the metadata-only fixture.
func BenchmarkMemoryProjectionIndexed(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) { benchmarkMemoryProjectionFixture(b, count, true) })
	}
}

func benchmarkMemoryProjectionFixture(b *testing.B, count int, indexed bool) {
	ctx := context.Background()
	directory := b.TempDir()
	project := "/audit-fixture"
	db, err := open(ctx, directory, project)
	if err != nil {
		b.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO documents(id,kind,session,sources,stamp) VALUES(?,'conversation','s','',1)`)
	if err != nil {
		b.Fatal(err)
	}
	for index := 0; index < count; index++ {
		if _, err := statement.Exec(fmt.Sprint(index)); err != nil {
			b.Fatal(err)
		}
	}
	statement.Close()
	if indexed {
		if _, err := tx.Exec(`INSERT INTO memory(id,text) SELECT id,'retained evidence' FROM documents`); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	db.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := Upsert(ctx, directory, project, Document{ID: "hot", Kind: "conversation", Session: "s", Text: "message", Stamp: 2}); err != nil {
			b.Fatal(err)
		}
	}
}
