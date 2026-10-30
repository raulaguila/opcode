package localstate

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LockFile serializes the entire read/change/write operation across processes.
// The sidecar stays in place: locking the replaced JSON inode loses exclusion
// after rename. Handles pin the private directory and reject symlink lock files.
func LockFile(path string) (func(), error) {
	unlockLocal := Lock(path)
	root, err := OpenPrivateRoot(filepath.Dir(path), true)
	if err != nil {
		unlockLocal()
		return nil, err
	}
	file, err := openLockFile(root, "."+filepath.Base(path)+".lock")
	if err == nil {
		err = waitFileLock(file)
	}
	if err != nil {
		if file != nil {
			file.Close()
		}
		root.Close()
		unlockLocal()
		return nil, err
	}
	return sync.OnceFunc(func() {
		// Closing releases the kernel lock even if an explicit unlock fails.
		file.Close()
		root.Close()
		unlockLocal()
	}), nil
}

func openLockFile(root *os.Root, name string) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && !before.Mode().IsRegular() {
		return nil, fmt.Errorf("state lock must be a regular private file")
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	after, pathErr := root.Lstat(name)
	if statErr != nil || pathErr != nil || !opened.Mode().IsRegular() || !after.Mode().IsRegular() || !os.SameFile(opened, after) || before != nil && !os.SameFile(before, opened) {
		file.Close()
		return nil, fmt.Errorf("state lock changed while opening")
	}
	return file, nil
}

func waitFileLock(file *os.File) error {
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		locked, err := tryFileLock(file)
		if err != nil || locked {
			return err
		}
		select {
		case <-timeout.C:
			return fmt.Errorf("state is busy in another instance; retry saving")
		case <-ticker.C:
		}
	}
}
