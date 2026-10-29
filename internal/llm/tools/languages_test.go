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
	}
	for _, f := range fixtures {
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
}
