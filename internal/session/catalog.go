package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/raulaguila/opcode/internal/localstate"
)

type LocalSession struct {
	Archived bool   `json:"archived,omitempty"`
	Draft    string `json:"draft,omitempty"`
}
type catalogDocument struct {
	Root     string                  `json:"root"`
	Last     string                  `json:"last,omitempty"`
	Sessions map[string]LocalSession `json:"sessions"`
}
type Catalog struct{ root, path string }

func NewCatalog(root, dataDir string) *Catalog {
	root, _ = filepath.Abs(root)
	if dataDir == "" {
		dataDir = filepath.Join(root, ".opcode")
	}
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(root, dataDir)
	}
	return &Catalog{root, filepath.Join(dataDir, "session-preferences.json")}
}
func (c *Catalog) load() (catalogDocument, error) {
	d := catalogDocument{Root: c.root, Sessions: map[string]LocalSession{}}
	err := localstate.Read(c.path, 1<<20, &d)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if d.Root != c.root || len(d.Sessions) > 500 {
		return d, fmt.Errorf("invalid session preferences or workspace mismatch")
	}
	if d.Sessions == nil {
		d.Sessions = map[string]LocalSession{}
	}
	return d, nil
}
func (c *Catalog) Snapshot() (map[string]LocalSession, string, error) {
	unlock := localstate.Lock(c.path)
	defer unlock()
	d, err := c.load()
	return d.Sessions, d.Last, err
}
func (c *Catalog) update(id string, change func(*LocalSession), active bool) error {
	if id == "" || len(id) > 128 || strings.ContainsFunc(id, unicode.IsControl) {
		return fmt.Errorf("invalid session identifier")
	}
	unlock := localstate.Lock(c.path)
	defer unlock()
	d, err := c.load()
	if err != nil {
		return err
	}
	entry := d.Sessions[id]
	change(&entry)
	if len(entry.Draft) > 64000 {
		return fmt.Errorf("session draft exceeds 64 KB")
	}
	d.Sessions[id] = entry
	if active {
		d.Last = id
	}
	if len(d.Sessions) > 500 {
		return fmt.Errorf("session preferences limit reached")
	}
	return localstate.Write(c.path, d, 1<<20)
}
func (c *Catalog) Archive(id string, archived bool) error {
	return c.update(id, func(s *LocalSession) { s.Archived = archived }, false)
}
func (c *Catalog) SaveDraft(id, draft string) error {
	return c.update(id, func(s *LocalSession) { s.Draft = draft }, true)
}
func (c *Catalog) Activate(id string) error { return c.update(id, func(*LocalSession) {}, true) }
