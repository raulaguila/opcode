package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/raulaguila/opcode/internal/workflow"
)

var ErrAffectedFileVerification = errors.New("affected-file verification failed")

func VerifyFileReceipts(ctx context.Context, files []workflow.FileReceipt) error {
	for _, file := range files {
		if err := VerifyFileFingerprint(ctx, file.Path, file.After); err != nil {
			return fmt.Errorf("%w for %s: %w", ErrAffectedFileVerification, file.Path, err)
		}
	}
	return nil
}

// VerifyFileFingerprint rechecks a previously approved effect, without recording
// source-read evidence or authorizing a new tool action. Empty means deleted.
func VerifyFileFingerprint(ctx context.Context, path, expected string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := canonicalPath(path)
	if err != nil {
		return err
	}
	if resolved != absolutePath(path) {
		return fmt.Errorf("affected file path changed through a symlink")
	}
	root, name, err := fileRoot(ctx, path, "read")
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if os.IsNotExist(err) && expected == "" {
		return nil
	}
	if err != nil {
		return err
	}
	if expected == "" || !info.Mode().IsRegular() {
		return fmt.Errorf("affected file no longer matches its verified tool result")
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, 16<<20+1))
	if err != nil {
		return err
	}
	if n > 16<<20 {
		return fmt.Errorf("affected file exceeds fingerprint limit (16 MiB)")
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return fmt.Errorf("affected file content changed after the tool completed")
	}
	return ctx.Err()
}
