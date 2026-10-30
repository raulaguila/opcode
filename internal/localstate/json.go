// Package localstate owns small private, bounded application documents.
package localstate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var locks sync.Map

func Lock(path string) func() {
	key, _ := filepath.Abs(path)
	lock, _ := locks.LoadOrStore(key, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// OpenPrivateRoot pins each directory from its filesystem volume. No ancestor
// symlink is followed, including links inside a configured storage directory.
// Existing directories retain their modes; newly created components are 0700.
// Callers own the returned handle. Reads should pass create=false.
func OpenPrivateRoot(directory string, create bool) (*os.Root, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	volume := filepath.VolumeName(absolute)
	base := volume + string(filepath.Separator)
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(base, absolute)
	if err != nil {
		root.Close()
		return nil, err
	}
	if relative == "." {
		return root, nil
	}
	for component := range strings.SplitSeq(relative, string(filepath.Separator)) {
		child, err := openPrivateComponent(root, component, create)
		root.Close()
		if err != nil {
			return nil, err
		}
		root = child
	}

	return root, nil
}
func Read(path string, limit int64, value any) error {
	if limit < 1 {
		return fmt.Errorf("application state read requires a positive limit")
	}
	root, err := OpenPrivateRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() > limit {
		return fmt.Errorf("invalid or oversized application state")
	}
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, opened) || !opened.Mode().IsRegular() || opened.Size() > limit {
		return fmt.Errorf("application state changed during access")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("application state exceeds limit")
	}
	if json.Unmarshal(data, value) != nil {
		return fmt.Errorf("invalid application state JSON")
	}
	return nil
}
func Write(path string, value any, limit int64) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if int64(len(data))+1 > limit {
		return fmt.Errorf("application state exceeds limit")
	}
	return WriteBytes(path, append(data, '\n'))
}
func WriteBytes(path string, data []byte) error {
	root, err := OpenPrivateRoot(filepath.Dir(path), true)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if exists && !before.Mode().IsRegular() {
		return fmt.Errorf("application state destination must be a regular file")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := ".opcode-state-" + hex.EncodeToString(nonce[:])
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	current, err := root.Lstat(name)
	if exists {
		if err != nil || !current.Mode().IsRegular() || !os.SameFile(before, current) {
			return fmt.Errorf("application state destination changed during write")
		}
	} else if !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		return fmt.Errorf("application state destination appeared during write")
	}
	return root.Rename(temp, name)
}

func openPrivateComponent(root *os.Root, component string, create bool) (*os.Root, error) {
	if component == "" || component == "." || component == ".." {
		return nil, fmt.Errorf("invalid private directory component")
	}
	before, err := root.Lstat(component)
	if os.IsNotExist(err) && create {
		if mkdirErr := root.Mkdir(component, 0700); mkdirErr != nil && !os.IsExist(mkdirErr) {
			return nil, mkdirErr
		}
		before, err = root.Lstat(component)
	}
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("private state directory cannot contain symbolic links or non-directories")
	}
	child, err := root.OpenRoot(component)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	after, afterErr := root.Lstat(component)
	if err != nil || afterErr != nil || !os.SameFile(before, opened) || !os.SameFile(before, after) || !after.IsDir() || after.Mode()&os.ModeSymlink != 0 {
		child.Close()
		return nil, fmt.Errorf("private state directory changed during access")
	}
	return child, nil
}
