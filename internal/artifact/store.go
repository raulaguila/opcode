// Package artifact owns bounded private snapshots of tool output. Snapshots are
// data, never current file-read evidence or permission grants.
package artifact

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/raulaguila/opcode/internal/localstate"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const MaxContent = 1 << 20
const MaxPage = 32768

var diskMu sync.Mutex
var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Store struct{ directory string }
type Info struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Bytes     int    `json:"bytes"`
	Created   int64  `json:"created"`
	Truncated bool   `json:"truncated"`
}
type entry struct {
	Info    Info   `json:"info"`
	Content string `json:"content"`
}
type Page struct {
	Info    Info   `json:"artifact"`
	Content string `json:"content"`
	Offset  int    `json:"offset"`
	Next    int    `json:"next_offset"`
	More    bool   `json:"more"`
}

func New(directory string) *Store { return &Store{directory: directory} }
func (s *Store) root() (*os.Root, error) {
	if s == nil || s.directory == "" {
		return nil, fmt.Errorf("artifact storage is unavailable")
	}
	root, err := localstate.OpenPrivateRoot(s.directory, true)
	if err != nil {
		return nil, err
	}
	if err := root.Chmod(".", 0700); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}
func sessionKey(session string) (string, error) {
	if session == "" {
		return "", fmt.Errorf("session is required")
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(session))), nil
}
func (s *Store) Put(ctx context.Context, session, label, content string) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	key, err := sessionKey(session)
	if err != nil {
		return Info{}, err
	}
	truncated := len(content) > MaxContent
	if truncated {
		content = content[:MaxContent]
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Info{}, err
	}
	info := Info{ID: hex.EncodeToString(random[:]), Label: label[:min(len(label), 160)], Bytes: len(content), Created: time.Now().Unix(), Truncated: truncated}
	raw, err := json.Marshal(entry{Info: info, Content: content})
	if err != nil {
		return Info{}, err
	}
	diskMu.Lock()
	defer diskMu.Unlock()
	root, err := s.root()
	if err != nil {
		return Info{}, err
	}
	defer root.Close()
	files, total, sessionBytes, sessionFiles := 0, int64(0), int64(0), 0
	visited := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > 2048 {
			return fmt.Errorf("artifact storage entry limit exceeded")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact storage contains a symbolic link")
		}
		if d.IsDir() {
			return nil
		}
		stat, err := d.Info()
		if err != nil {
			return err
		}
		files++
		total += stat.Size()
		if strings.HasPrefix(path, key+"-") {
			sessionBytes += stat.Size()
			sessionFiles++
		}
		if files > 1024 || total > 64<<20 {
			return fmt.Errorf("artifact storage quota exceeded")
		}
		return nil
	})
	if err != nil {
		return Info{}, err
	}
	if files >= 1024 || total+int64(len(raw)) > 64<<20 || sessionFiles >= 32 || sessionBytes+int64(len(raw)) > 8<<20 {
		return Info{}, fmt.Errorf("artifact storage quota exceeded")
	}
	f, err := root.OpenFile(key+"-"+info.ID+".json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Info{}, err
	}
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		root.Remove(key + "-" + info.ID + ".json")
		if writeErr != nil {
			return Info{}, writeErr
		}
		return Info{}, closeErr
	}
	return info, nil
}
func (s *Store) get(ctx context.Context, session, id string) (entry, error) {
	if err := ctx.Err(); err != nil {
		return entry{}, err
	}
	if !validID.MatchString(id) {
		return entry{}, fmt.Errorf("invalid artifact ID")
	}
	key, err := sessionKey(session)
	if err != nil {
		return entry{}, err
	}
	root, err := s.root()
	if err != nil {
		return entry{}, err
	}
	defer root.Close()
	name := key + "-" + id + ".json"
	stat, err := root.Lstat(name)
	if err != nil {
		return entry{}, fmt.Errorf("artifact not found in this session")
	}
	if !stat.Mode().IsRegular() {
		return entry{}, fmt.Errorf("invalid artifact file")
	}
	f, err := root.Open(name)
	if err != nil {
		return entry{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(stat, opened) {
		return entry{}, fmt.Errorf("artifact changed during read")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 6*MaxContent+4097))
	if err != nil {
		return entry{}, err
	}
	var e entry
	if len(raw) > 6*MaxContent+4096 || json.Unmarshal(raw, &e) != nil || e.Info.ID != id || len(e.Content) > MaxContent {
		return entry{}, fmt.Errorf("invalid artifact contents")
	}
	return e, nil
}
func (s *Store) Info(ctx context.Context, session, id string) (Info, error) {
	e, err := s.get(ctx, session, id)
	return e.Info, err
}
func (s *Store) Read(ctx context.Context, session, id string, offset, limit int) (Page, error) {
	e, err := s.get(ctx, session, id)
	if err != nil {
		return Page{}, err
	}
	if offset < 0 || offset > len(e.Content) {
		return Page{}, fmt.Errorf("invalid artifact offset")
	}
	if limit <= 0 {
		limit = 8192
	}
	limit = min(limit, MaxPage)
	end := min(offset+limit, len(e.Content))
	return Page{Info: e.Info, Content: e.Content[offset:end], Offset: offset, Next: end, More: end < len(e.Content)}, nil
}
func (s *Store) Search(ctx context.Context, session, id, query string, offset, limit int) ([]Page, error) {
	if query == "" || len(query) > 500 || offset < 0 {
		return nil, fmt.Errorf("query requires 1–500 bytes and a nonnegative offset")
	}
	e, err := s.get(ctx, session, id)
	if err != nil {
		return nil, err
	}
	if offset > len(e.Content) {
		return nil, fmt.Errorf("invalid artifact offset")
	}
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	result := []Page{}
	for offset < len(e.Content) && len(result) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		at := strings.Index(e.Content[offset:], query)
		if at < 0 {
			break
		}
		at += offset
		start, end := max(0, at-120), min(len(e.Content), at+len(query)+120)
		result = append(result, Page{Info: e.Info, Content: e.Content[start:end], Offset: start, Next: at + len(query), More: at+len(query) < len(e.Content)})
		offset = at + len(query)
	}
	return result, nil
}
