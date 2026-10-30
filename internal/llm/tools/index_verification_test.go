package tools

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOversizedStaleIndexCandidateDoesNotHideValidHits(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "stale.txt", "original index source\n")
	retrievalFixture(t, root, "valid.txt", "valid index source\n")
	stale := codeIndexHit{Path: "stale.txt", Hash: indexDigest("original index source\n"), Start: 1, End: 1}
	valid := codeIndexHit{Path: "valid.txt", Hash: indexDigest("valid index source\n"), Start: 1, End: 1}
	for _, size := range []int64{indexLimits().MaxFileBytes + 1, 17 << 20} {
		require.NoError(t, os.Truncate(filepath.Join(root, "stale.txt"), size))
		items, _, err := verifiedCodeIndexHits(toolContext(), root, []codeIndexHit{stale, valid}, 6)
		require.NoError(t, err)
		require.Equal(t, []codeIndexHit{valid}, items)
	}
}

func TestIndexDigestStreamingBoundsAndCancellation(t *testing.T) {
	root := productivityV2Workspace(t)
	path := filepath.Join(root, "source.txt")
	raw := bytes.Repeat([]byte("x"), int(indexLimits().MaxFileBytes))
	require.NoError(t, os.WriteFile(path, raw, 0600))
	digest, allowed, err := verifiedIndexDigest(toolContext(), path, map[string]string{})
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, indexDigest(string(raw)), digest)
	// A file that grows after its initial stat is bounded to the limit plus one.
	limited := io.LimitReader(bytes.NewReader(raw), 129)
	_, allowed, err = streamIndexDigest(toolContext(), limited, 128, 16)
	require.NoError(t, err)
	require.False(t, allowed)
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	_, _, err = verifiedIndexDigest(ctx, path, map[string]string{path: digest})
	require.ErrorIs(t, err, context.Canceled)
}

func TestIndexDigestRetainsAuthorizationAndSymlinkChecks(t *testing.T) {
	root := productivityV2Workspace(t)
	path := filepath.Join(root, "source.txt")
	retrievalFixture(t, root, "source.txt", "fixture\n")
	link := filepath.Join(root, "link.txt")
	require.NoError(t, os.Symlink(path, link))
	_, allowed, err := verifiedIndexDigest(toolContext(), link, map[string]string{})
	require.NoError(t, err)
	require.False(t, allowed)
	sensitive := filepath.Join(root, ".env")
	require.NoError(t, os.WriteFile(sensitive, []byte("private"), 0600))
	ctx := context.WithValue(toolContext(), accessKey{}, &accessPolicy{tool: "index-verification", approved: map[string]bool{}})
	_, _, err = verifiedIndexDigest(ctx, sensitive, map[string]string{})
	require.Error(t, err)
	failure := errors.New("read failed")
	_, _, err = streamIndexDigest(ctx, failingIndexReader{err: failure}, 128, 16)
	require.ErrorIs(t, err, failure)
}

type failingIndexReader struct{ err error }

func (r failingIndexReader) Read([]byte) (int, error) { return 0, r.err }
