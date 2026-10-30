package evaluation

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestAdapterContractAndRetrievalMetrics(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "adapter.json")
	raw, _ := json.Marshal(Adapter{Name: "codaro", Command: "/usr/bin/python3", Args: []string{"driver.py"}})
	require.NoError(t, os.WriteFile(path, raw, 0600))
	a, err := LoadAdapter(path)
	require.NoError(t, err)
	require.Equal(t, "codaro", a.Name)
	require.Equal(t, "search_code_index", canonicalTool("codaro", "search_code"))
	require.Equal(t, "patch", canonicalTool("crush", "multiedit"))
	recall, mrr := retrievalQuality([]string{"a.py", "b.py"}, []string{"decoy.ts", "b.py", "b.py"})
	require.Equal(t, .5, recall)
	require.Equal(t, .5, mrr)
	raw, _ = json.Marshal(Adapter{Name: "unknown", Command: "sh"})
	require.NoError(t, os.WriteFile(path, raw, 0600))
	_, err = LoadAdapter(path)
	require.Error(t, err)
}

// Compare the former per-observation compilation with the shared immutable
// matcher using identical input and expression; this does not change criteria.
func BenchmarkAdapterVersionValidation(b *testing.B) {
	const version = "v1.2.3-fixture"
	b.Run("compile_per_observation", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			regexp.MustCompile(adapterVersionPattern.String()).MatchString(version)
		}
	})
	b.Run("compiled_once", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			adapterVersionPattern.MatchString(version)
		}
	})
}
