package extensions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExplicitSkillCatalogueAndChangedSource(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inspect")
	require.NoError(t, os.Mkdir(dir, 0700))
	path := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte("---\nname: inspect\ndescription: Read selected code\n---\nUse view. Never grant permissions."), 0600))
	skills, err := Discover(context.Background(), []string{root})
	require.NoError(t, err)
	require.Len(t, skills, 1)
	require.Equal(t, "inspect", skills[0].Name)
	require.Equal(t, "Read selected code", skills[0].Description)
	skill, text, err := Load(context.Background(), []string{root}, "inspect")
	require.NoError(t, err)
	require.Contains(t, text, "Never grant permissions")
	require.Equal(t, skills[0].SHA256, skill.SHA256)
	require.NoError(t, os.WriteFile(path, []byte("---\nname: inspect\n---\nChanged guidance"), 0600))
	fresh, _, err := Load(context.Background(), []string{root}, "inspect")
	require.NoError(t, err)
	require.NotEqual(t, skill.SHA256, fresh.SHA256)
	_, _, err = Load(context.Background(), nil, "inspect")
	require.Error(t, err)
}
func TestSkillBoundsSymlinksAndCancellation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "large")
	require.NoError(t, os.Mkdir(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(strings.Repeat("x", MaxSkillBytes+1)), 0600))
	_, err := Discover(context.Background(), []string{root})
	require.ErrorContains(t, err, "16 KiB")
	require.NoError(t, os.Remove(filepath.Join(dir, "SKILL.md")))
	outside := filepath.Join(t.TempDir(), "guide.md")
	require.NoError(t, os.WriteFile(outside, []byte("private"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "SKILL.md")))
	_, err = Discover(context.Background(), []string{root})
	require.ErrorContains(t, err, "regular file")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Discover(ctx, []string{root})
	require.ErrorIs(t, err, context.Canceled)
}

func TestSkillDirectoryReadIsBoundedBeforeInspectingChildren(t *testing.T) {
	root := t.TempDir()
	for i := range 257 {
		require.NoError(t, os.Mkdir(filepath.Join(root, fmt.Sprintf("skill-%03d", i)), 0700))
	}
	_, err := Discover(context.Background(), []string{root})
	require.ErrorContains(t, err, "256 entries")
}
