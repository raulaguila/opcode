package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalLanguageDeclarations(t *testing.T) {
	fixtures := []struct{ path, source, symbol string }{
		{"service.go", "package service\ntype Ledger struct{}\nfunc (l Ledger) Total() int {return 37}\n", "Ledger.Total"},
		{"service.py", "class Ledger:\n    def total(self):\n        return 37\n", "Ledger.total"},
		{"service.js", "class Ledger { total() { return 37; } }\n", "Ledger.total"},
		{"service.ts", "class Ledger { total(): number { return 37; } }\n", "Ledger.total"},
		{"service.tsx", "function Ledger() { return <div>37</div>; }\n", "Ledger"},
		{"service.rs", "struct Ledger {}\nimpl Ledger { fn total(&self) -> i32 { 37 } }\n", "Ledger.total"},
		{"Ledger.java", "class Ledger { int total() { return 37; } }\n", "Ledger.total"},
		{"service.c", "int total(void) { return 37; }\n", "total"},
		{"service.cpp", "class Ledger { public: int total() { return 37; } };\n", "Ledger.total"},
		{"Ledger.cs", "class Ledger { int Total() { return 37; } }\n", "Ledger.Total"},
		{"service.rb", "class Ledger\n  def total\n    37\n  end\nend\n", "Ledger.total"},
		{"service.php", "<?php\nclass Ledger { function total() { return 37; } }\n", "Ledger.total"},
		{"service.kt", "class Ledger {\n fun total(): Int {\n  return 37\n }\n}\n", "Ledger.total"},
		{"service.swift", "class Ledger { func total() -> Int { return 37 } }\n", "Ledger.total"},
		{"schema.sql", "CREATE TABLE ledger (total INTEGER DEFAULT 37);\n", "ledger"},
		{"service.sh", "total() { echo 37; }\n", "total"},
		{"service.lua", "function total() return 37 end\n", "total"},
		{"service.dart", "class Ledger {\n  int total() {\n    return 37;\n  }\n}\n", "Ledger.total"},
		{"Ledger.vue", "<script lang=\"ts\">\nfunction total(): number {\n  return 37;\n}\n</script>\n<template><p>37</p></template>\n", "total"},
		{"Ledger.svelte", "<script>\nfunction total() {\n  return 37;\n}\n</script>\n<p>37</p>\n", "total"},
		{"ledger.tf", "resource \"thing\" \"ledger\" {\n total = 37\n}\n", "resource.thing.ledger"},
		{"ledger.yaml", "ledger:\n  total: 37\n", "ledger.total"},
		{"ledger.toml", "[ledger]\ntotal = 37\n", "ledger.total"},
		{"ledger.json", "{\"ledger\": {\"total\":37}}\n", "ledger.total"},
		{"ledger.html", "<section id=\"ledger\">37</section>\n", "#ledger"},
		{"ledger.css", ".ledger { --total: 37; }\n", ".ledger"},
	}
	covered := map[string]bool{}
	for _, f := range fixtures {
		covered[sourceLanguage(f.path)] = true
		t.Run(f.path, func(t *testing.T) {
			symbols, err := localOutline(context.Background(), f.path, []byte(f.source))
			require.NoError(t, err)
			names := []string{}
			for _, s := range symbols {
				names = append(names, s.Name)
				require.GreaterOrEqual(t, s.Start, 1)
				require.GreaterOrEqual(t, s.End, s.Start)
			}
			require.Contains(t, names, f.symbol)
			found := false
			for _, c := range codeChunks(f.path, f.source) {
				if c.Symbol == f.symbol {
					found = true
					require.Contains(t, c.Content, "37")
				}
			}
			require.True(t, found, "structural declaration must reach index chunks: %s", strings.Join(names, ","))
		})
	}
	require.Len(t, covered, len(localParsers)+1, "each registered grammar must have a declaration and index-chunk fixture")
	for language := range localParsers {
		require.True(t, covered[language], "missing grammar fixture: %s", language)
	}
}

func TestLanguageOutlineBoundsAndEmbeddedRanges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := localOutline(ctx, "service.rb", []byte("class Ledger; end"))
	require.ErrorIs(t, err, context.Canceled)
	_, err = localOutline(context.Background(), "service.sql", []byte(strings.Repeat("x", 256*1024+1)))
	require.ErrorContains(t, err, "256 KiB")
	_, err = localOutline(context.Background(), "service.unknown", []byte("opaque source"))
	require.ErrorContains(t, err, "no local structural parser")
	for _, path := range []string{"Ledger.vue", "Ledger.svelte"} {
		symbols, err := localOutline(context.Background(), path, []byte("<script lang=\"ts\">\nfunction total(): number {\n return 37;\n}\n</script>"))
		require.NoError(t, err)
		require.Equal(t, []codeSymbol{{"total", "function", 2, 4}}, symbols, "embedded locations must refer to original file")
	}
	_, err = localOutline(context.Background(), "Ledger.vue", []byte(strings.Repeat("<script>function total() { return 37; }</script>\n", 9)))
	require.ErrorContains(t, err, "embedded script outline limit")
	chunks := codeChunks("invalid.rb", "class Ledger\n  def total(\n")
	require.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		require.Equal(t, "<file>", chunk.Symbol, "malformed syntax must fall back to text")
	}
}
