package tools

import (
	"fmt"
	"os"
	"path/filepath"

	ignore "github.com/sabhiram/go-gitignore"
)

// Policy snapshots last for one discovery/refresh operation. Every path still
// checks sensitive names and symlinks; a changed ignore file rejects the scan.
type discoveryPolicy struct {
	files   map[string]policyFile
	failure error
}
type policyFile struct {
	stamp   string
	matcher *ignore.GitIgnore
	err     error
}

func newDiscoveryPolicy() *discoveryPolicy {
	return &discoveryPolicy{files: make(map[string]policyFile)}
}

func policyStamp(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	return indexFileStamp(info), nil
}

func (p *discoveryPolicy) matcher(dir, name string) (*ignore.GitIgnore, error) {
	if p.failure != nil {
		return nil, p.failure
	}
	path := filepath.Join(dir, name)
	if cached, ok := p.files[path]; ok {
		return cached.matcher, cached.err
	}
	before, err := policyStamp(path)
	if err != nil {
		return nil, err
	}
	matcher, err := directoryIgnore(dir, name)
	after, statErr := policyStamp(path)
	if statErr != nil || before != after {
		p.failure = fmt.Errorf("ignore policy changed; retry discovery")
		return nil, p.failure
	}
	// Bound retained metadata even in workspaces with very many directories.
	if len(p.files) >= 65536 {
		p.failure = fmt.Errorf("ignore policy snapshot exceeds 65536 entries; narrow the search scope")
		return nil, p.failure
	}
	p.files[path] = policyFile{stamp: after, matcher: matcher, err: err}
	return matcher, err
}

func (p *discoveryPolicy) Validate() error {
	if p.failure != nil {
		return p.failure
	}
	for path, prior := range p.files {
		stamp, err := policyStamp(path)
		if err != nil || stamp != prior.stamp {
			return fmt.Errorf("ignore policy changed; retry discovery")
		}
	}
	return nil
}

func (p *discoveryPolicy) checkedPaths(paths []string, truncated bool) ([]string, bool, error) {
	if err := p.Validate(); err != nil {
		return nil, false, err
	}
	return paths, truncated, nil
}

func (p *discoveryPolicy) exclusionReason(root, path string) string {
	return pathExclusionReason(root, path, p.matcher)
}
