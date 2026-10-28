package diff

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/aymanbagabas/go-udiff"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func diffConfig(t *testing.T) {
	t.Helper()
	if config.Get() != nil {
		return
	}
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
	viper.SetConfigFile(path)
	_, err := config.Load(root, false)
	require.NoError(t, err)
}
func TestGenerateDiffDisplaysBinaryBytesWithoutFatalExit(t *testing.T) {
	diffConfig(t)
	for _, test := range []struct{ name, before, after string }{{"invalid_utf8", string([]byte{0xff, 'a'}), string([]byte{0xfe, 'b'})}, {"nul", "secret\x00before", "secret\x00after"}, {"binary_to_text", string([]byte{0xff}), "plain text\n"}} {
		t.Run(test.name, func(t *testing.T) {
			result, added, removed := GenerateDiff(test.before, test.after, "bytes.dat")
			require.True(t, utf8.ValidString(result))
			require.Contains(t, result, "conteúdo binário")
			require.Contains(t, result, fmt.Sprintf("SHA256 %x", sha256.Sum256([]byte(test.before))))
			require.NotContains(t, result, "\x00")
			require.Positive(t, added)
			require.Positive(t, removed)
		})
	}
	same := string([]byte{0xff, 0})
	result, added, removed := GenerateDiff(same, same, "same.dat")
	require.Empty(t, result)
	require.Zero(t, added)
	require.Zero(t, removed)
}
func TestGenerateDiffPreservesASCIIUnifiedOutput(t *testing.T) {
	diffConfig(t)
	before, after := "one\nshared\n", "two\nshared\n"
	result, added, removed := GenerateDiff(before, after, "file.txt")
	require.Equal(t, udiff.Unified("a/file.txt", "b/file.txt", before, after), result)
	require.Equal(t, 1, added)
	require.Equal(t, 1, removed)
	require.NotContains(t, result, "conteúdo binário")
}
