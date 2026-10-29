package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReleaseTargetsRejectDuplicatesAndUnsupportedPlatforms(t *testing.T) {
	expected := []string{"linux/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}
	actual, err := releaseTargets("linux/amd64,darwin/arm64,windows/amd64,windows/arm64")
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	for _, invalid := range []string{"", "windows/386", "linux/amd64,linux/amd64", "../../amd64", "plan9/amd64", "windows/amd64,"} {
		_, err := releaseTargets(invalid)
		require.Error(t, err)
	}
}
func TestReleaseArchivesAreReproducibleAndIncludeOnlyPublicFiles(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	require.NoError(t, os.WriteFile("README.md", []byte("fixture readme"), 0644))
	require.NoError(t, os.WriteFile("LICENSE", []byte("fixture license"), 0644))
	require.NoError(t, os.WriteFile("private-config.json", []byte("fake-secret-never-archive"), 0600))
	binary := filepath.Join(root, "built-binary")
	require.NoError(t, os.WriteFile(binary, []byte("binary fixture bytes"), 0755))
	for _, format := range []string{"tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			first, second := filepath.Join(root, "one."+format), filepath.Join(root, "two."+format)
			build := archive
			if format == "zip" {
				build = archiveZIP
			}
			require.NoError(t, build(first, binary))
			require.NoError(t, os.Chtimes(binary, time.Now(), time.Now()))
			require.NoError(t, build(second, binary))
			a, err := os.ReadFile(first)
			require.NoError(t, err)
			b, err := os.ReadFile(second)
			require.NoError(t, err)
			require.Equal(t, a, b, "source mtimes must not affect release bytes")
			names := []string{}
			if format == "zip" {
				reader, err := zip.OpenReader(first)
				require.NoError(t, err)
				defer reader.Close()
				for _, file := range reader.File {
					names = append(names, file.Name)
					require.Equal(t, 1980, file.Modified.Year())
					input, err := file.Open()
					require.NoError(t, err)
					data, err := io.ReadAll(input)
					require.NoError(t, err)
					require.NoError(t, input.Close())
					require.NotContains(t, string(data), "fake-secret-never-archive")
				}
				require.Equal(t, []string{"opcode.exe", "LICENSE", "README.md"}, names)
			} else {
				file, err := os.Open(first)
				require.NoError(t, err)
				defer file.Close()
				gzipReader, err := gzip.NewReader(file)
				require.NoError(t, err)
				defer gzipReader.Close()
				reader := tar.NewReader(gzipReader)
				for {
					header, err := reader.Next()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					names = append(names, header.Name)
					require.Equal(t, int64(0), header.ModTime.Unix())
				}
				require.Equal(t, []string{"opcode", "LICENSE", "README.md"}, names)
			}
		})
	}
}
