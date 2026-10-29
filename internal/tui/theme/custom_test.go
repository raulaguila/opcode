package theme

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/stretchr/testify/require"
)

func isolateCustomTheme(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	original := CurrentThemeName()
	saved := GetTheme(CustomThemeName)
	t.Cleanup(func() {
		_ = DiscardCustomPreview(original)
		globalManager.mu.Lock()
		if saved == nil {
			delete(globalManager.themes, CustomThemeName)
		} else {
			globalManager.themes[CustomThemeName] = saved
		}
		globalManager.mu.Unlock()
	})
}
func TestCustomPalettePreviewIsReversibleAndDoesNotPersist(t *testing.T) {
	isolateCustomTheme(t)
	base, err := PaletteFor("opcode")
	require.NoError(t, err)
	original := CurrentThemeName()
	require.NoError(t, ValidateCustomPalette(base))
	base.Primary.Dark = "#ffb283"
	require.NoError(t, PreviewCustomPalette(base))
	require.Equal(t, base.Primary, CurrentTheme().Primary())
	require.Equal(t, GetTheme("opcode").SyntaxKeyword(), CurrentTheme().SyntaxKeyword(), "syntax colors stay inherited")
	path, err := CustomThemePath()
	require.NoError(t, err)
	require.NoFileExists(t, path)
	require.NoError(t, DiscardCustomPreview(original))
	require.Equal(t, original, CurrentThemeName())
	require.Nil(t, GetTheme(customPreviewName))
}
func TestCustomPaletteReloadRejectsUnsafeOrOversizedDocuments(t *testing.T) {
	isolateCustomTheme(t)
	p, err := PaletteFor("opcode")
	require.NoError(t, err)
	p.Primary.Dark = "#ffb283"
	path, err := CustomThemePath()
	require.NoError(t, err)
	require.NoError(t, LoadCustomTheme())
	require.NoError(t, localstate.Write(path, p, customThemeLimit))
	require.NoError(t, LoadCustomTheme())
	loaded, err := PaletteFor(CustomThemeName)
	require.NoError(t, err)
	require.Equal(t, p, loaded)
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", customThemeLimit+1)), 0600))
	require.Error(t, LoadCustomTheme())
	loaded, err = PaletteFor(CustomThemeName)
	require.NoError(t, err)
	require.Equal(t, p, loaded, "invalid reload must preserve previous registered palette")
	if runtime.GOOS == "windows" {
		return
	}
	require.NoError(t, os.Remove(path))
	target := filepath.Join(t.TempDir(), "outside.json")
	require.NoError(t, os.WriteFile(target, []byte(`{}`), 0600))
	require.NoError(t, os.Symlink(target, path))
	require.Error(t, LoadCustomTheme())
	require.Error(t, localstate.Write(path, p, customThemeLimit))
}
func TestCustomPaletteRequiresLiteralColorAndReadablePairs(t *testing.T) {
	isolateCustomTheme(t)
	p, err := PaletteFor("opcode")
	require.NoError(t, err)
	for _, bad := range []string{"red", "#fff", "$(touch bad)", "#GGGGGG", "#123456\x1b[2J"} {
		candidate := p
		candidate.Primary.Dark = bad
		require.Error(t, ValidateCustomPalette(candidate))
	}
	candidate := p
	candidate.Text.Dark = candidate.Background.Dark
	require.ErrorContains(t, ValidateCustomPalette(candidate), "contrast")
	candidate = p
	candidate.Base = CustomThemeName
	require.Error(t, ValidateCustomPalette(candidate))
}
