package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalLanguageDeclarations(t *testing.T) {
	fixtures := []struct{ path, source, symbol string }{

		{"service.scala", "object Ledger { def total(): Int = 37 }\n", "Ledger.total"},
		{"service.ex", "defmodule Ledger do\n def total(), do: 37\nend\n", "Ledger.total"},
		{"service.erl", "-module(ledger).\ntotal() -> 37.\n", "total"},
		{"service.hs", "total :: Int\ntotal = 37\n", "total"},
		{"service.ml", "let total = 37\n", "total"},
		{"service.r", "total <- function() { 37 }\n", "total"},
		{"service.jl", "function total()\n return 37\nend\n", "total"},
		{"service.pl", "sub total { return 37; }\n", "total"},
		{"service.ps1", "function total { return 37 }\n", "total"},
		{"service.clj", "(defn total [] 37)\n", "total"},
		{"service.fs", "module Ledger\nlet total = 37\n", "Ledger.total"},
		{"service.f90", "module ledger\ncontains\ninteger function total()\n total = 37\nend function total\nend module ledger\n", "ledger.total"},
		{"service.zig", "pub fn total() i32 { return 37; }\n", "total"},
		{"service.sol", "pragma solidity ^0.8.0;\ncontract Ledger { function total() public pure returns (uint) { return 37; } }\n", "Ledger.total"},
		{"service.proto", "syntax = \"proto3\";\nmessage Ledger { int32 total = 37; }\n", "Ledger"},
		{"service.graphql", "type Ledger { total: Int # 37\n}\n", "Ledger"},

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
	ranges := map[string][2]int{"scala": {1, 1}, "elixir": {2, 2}, "erlang": {2, 2}, "haskell": {2, 2}, "ocaml": {1, 1}, "r": {1, 1}, "julia": {1, 3}, "perl": {1, 1}, "powershell": {1, 1}, "clojure": {1, 1}, "fsharp": {2, 2}, "fortran": {3, 5}, "zig": {1, 1}, "solidity": {2, 2}, "proto": {2, 2}, "graphql": {1, 2}}
	covered := map[string]bool{}
	for _, f := range fixtures {
		covered[sourceLanguage(f.path)] = true
		t.Run(f.path, func(t *testing.T) {
			symbols, err := localOutline(context.Background(), f.path, []byte(f.source))
			require.NoError(t, err)
			names := []string{}
			for _, s := range symbols {
				names = append(names, s.Name)
				if expected, ok := ranges[sourceLanguage(f.path)]; ok && s.Name == f.symbol {
					require.Equal(t, expected, [2]int{s.Start, s.End}, "declaration range must use original source lines")
				}
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

func TestExpressionShapedNodesAreNotDeclarations(t *testing.T) {
	for _, f := range []struct{ path, source string }{{"ordinary.ex", "total(37)\n"}, {"ordinary.clj", "(map inc [37])\n"}, {"ordinary.r", "(function() {37})()\n"}} {
		t.Run(f.path, func(t *testing.T) {
			symbols, err := localOutline(context.Background(), f.path, []byte(f.source))
			require.NoError(t, err)
			require.Empty(t, symbols)
		})
	}
}
