package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/permission"
)

type accessKey struct{}
type accessPolicy struct {
	permissions permission.Service
	tool        string
	approved    map[string]bool
}
type secureTool struct {
	BaseTool
	permissions permission.Service
}

func SecureTool(tool BaseTool, permissions permission.Service) BaseTool {
	if wrapped, ok := tool.(*secureTool); ok {
		tool = wrapped.BaseTool
	}
	return &secureTool{BaseTool: tool, permissions: permissions}
}
func (s *secureTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	if redact(call.Input) != call.Input {
		return NewTextErrorResponse("tool arguments contain a configured credential; remove it before retrying"), nil
	}
	ctx = context.WithValue(ctx, accessKey{}, &accessPolicy{permissions: s.permissions, tool: s.Info().Name, approved: map[string]bool{}})
	if err := authorizeToolTraversal(ctx, s.Info().Name, call.Input); err != nil {
		return ToolResponse{}, err
	}
	response, err := s.BaseTool.Run(ctx, call)
	response.Content = redact(response.Content)
	if len(response.Content) > 16384 && !strings.Contains(response.Content, "[Command output saved as artifact]") && s.Info().Name != "read_artifact" && s.Info().Name != "search_artifact" {
		if snapshot, snapshotErr := SnapshotOutput(ctx, s.Info().Name, response.Content); snapshotErr == nil {
			response.Content = "[Output saved as artifact; inspect with read_artifact/search_artifact]\n" + snapshot + "\n" + response.Content[:8192]
		}
	}
	response.Metadata = redact(response.Metadata)
	if err != nil {
		return response, &toolError{name: s.Info().Name, cause: err}
	}
	return response, nil
}
func credentialValues() []string {
	var secrets []string
	for _, entry := range os.Environ() {
		key, secret, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if len(secret) >= 8 && (strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "API_KEY")) {
			secrets = append(secrets, secret)
		}
	}
	secrets = append(secrets, config.CredentialValuesSnapshot()...)
	return secrets
}
func RedactCredentials(value string) string { return redact(value) }

func redact(value string) string {
	for _, secret := range credentialValues() {
		value = redactValue(value, secret)
	}
	return value
}

// A live snapshot can end in the first bytes of a credential. Hide suffixes
// until the process completes, so splitting writes never exposes a key prefix.
func redactLive(value string, running bool) string {
	value = redact(value)
	if !running {
		return value
	}
	for _, secret := range credentialValues() {
		for _, candidate := range []string{secret, strings.Trim(string(mustJSON(secret)), "\"")} {
			for n := min(len(candidate)-1, len(value)); n > 0; n-- {
				if strings.HasSuffix(value, candidate[:n]) {
					value = value[:len(value)-n] + "[redacted]"
					break
				}
			}
		}
	}
	return value
}
func mustJSON(value string) []byte { result, _ := json.Marshal(value); return result }

// Terminal wrapping inserts row separators inside keys. Match a compact view
// and mask the corresponding screen bytes, including incomplete trailing keys.
func redactScreen(value string, running bool) string {
	raw := []byte(value)
	var compact []byte
	var positions []int
	for i, b := range raw {
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		compact = append(compact, b)
		positions = append(positions, i)
	}

	masker := screenCredentialMasker{raw: raw, positions: positions, text: string(compact)}
	for _, secret := range credentialValues() {
		masker.hide(secret, running)
	}
	return redact(string(raw))
}
func absolutePath(path string) string {
	if path == "" {
		return config.WorkingDirectory()
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(config.WorkingDirectory(), path)
	}
	return filepath.Clean(path)
}
func canonicalPath(path string) (string, error) {
	path = absolutePath(path)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for _, s := range slices.Backward(suffix) {
				resolved = filepath.Join(resolved, s)
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}
func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func authorizeFile(ctx context.Context, path, action string) (string, error) {
	resolved, err := canonicalPath(path)
	if err != nil {
		return "", err
	}
	policy, _ := ctx.Value(accessKey{}).(*accessPolicy)
	if policy == nil {
		return resolved, nil
	}
	root, err := canonicalPath(config.WorkingDirectory())
	if err != nil {
		return "", err
	}
	sensitive := sensitiveAccessPath(path, resolved)
	symlinkWrite := action == "write" && resolved != absolutePath(path)
	if inside(root, resolved) && !sensitive && !symlinkWrite {
		return resolved, nil
	}
	kind := action + "_external"
	if sensitive {
		kind = action + "_sensitive"
	} else if symlinkWrite {
		kind = "write_symlink"
	}
	key := kind + ":" + resolved
	if policy.approved[key] {
		return resolved, nil
	}
	session, _ := GetContextValues(ctx)
	if policy.permissions == nil || session == "" || !policy.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: session, ToolName: policy.tool, Action: kind, Path: resolved, Description: "Access " + kind + " path: " + resolved}) {
		return "", permission.ErrorPermissionDenied
	}
	policy.approved[key] = true
	return resolved, nil
}
func fileRoot(ctx context.Context, path, action string) (*os.Root, string, error) {
	path, err := authorizeFile(ctx, path, action)
	if err != nil {
		return nil, "", err
	}
	rootPath, err := canonicalPath(config.WorkingDirectory())
	if err != nil {
		return nil, "", err
	}
	if !inside(rootPath, path) {
		rootPath = filepath.Dir(path)
		for {
			if _, err := os.Stat(rootPath); err == nil {
				break
			}
			parent := filepath.Dir(rootPath)
			if parent == rootPath {
				break
			}
			rootPath = parent
		}
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, "", err
	}
	name, err := filepath.Rel(rootPath, path)
	if err != nil {
		root.Close()
		return nil, "", err
	}
	return root, name, nil
}
func openFile(ctx context.Context, path string) (*os.File, error) {
	root, name, err := fileRoot(ctx, path, "read")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("only regular files can be read")
	}
	return root.Open(name)
}
func readFile(ctx context.Context, path string) ([]byte, error) {
	f, err := openFile(ctx, path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if len(raw) > 16<<20 {
		return nil, errors.New("file exceeds 16 MiB edit limit")
	}
	return raw, err
}
func statFile(ctx context.Context, path string) (os.FileInfo, error) {
	root, name, err := fileRoot(ctx, path, "read")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Stat(name)
}

func (s *secureTool) Close() error {
	if closer, ok := s.BaseTool.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
func (s *secureTool) CancelSession(session string) {
	if closer, ok := s.BaseTool.(interface{ CancelSession(string) }); ok {
		closer.CancelSession(session)
	}
}

func redactValue(value, secret string) string {
	value = strings.ReplaceAll(value, secret, "[REDACTED]")
	encoded, _ := json.Marshal(secret)
	if len(encoded) > 2 {
		value = strings.ReplaceAll(value, string(encoded[1:len(encoded)-1]), "[REDACTED]")
	}
	return value
}

type toolError struct {
	name  string
	cause error
}

func (e *toolError) Error() string { return e.name + ": " + redact(e.cause.Error()) }
func (e *toolError) Unwrap() error { return e.cause }

// IsOpaqueTool identifies integrations independently of names controlled by a
// remote server. A server cannot become read-only by impersonating a native tool.
func IsOpaqueTool(tool BaseTool) bool {
	for {
		wrapped, ok := tool.(*secureTool)
		if !ok {
			break
		}
		tool = wrapped.BaseTool
	}
	if opaque, ok := tool.(interface{ OpaqueTool() bool }); ok {
		return opaque.OpaqueTool()
	}
	return false
}

func authorizeToolTraversal(ctx context.Context, tool, raw string) error {
	// Scope directory traversal before starting subprocesses or walking trees.
	var input struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	localPathTool := false
	switch tool {
	case "view", "ls", "glob", "grep", "edit", "write", "patch", "diagnostics", "symbols", "definition", "references":
		localPathTool = true
	}
	if localPathTool && json.Unmarshal([]byte(raw), &input) == nil {
		path := input.FilePath
		if path == "" {
			path = input.Path
		}
		if path != "" {
			if _, err := authorizeFile(ctx, path, "read"); err != nil {
				return err
			}
		}
	}
	return nil
}

func sensitiveAccessPath(path, resolved string) bool {
	sensitive := shell.SensitivePath(resolved) || shell.SensitivePath(absolutePath(path))
	if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" {
		if dataRoot, e := canonicalPath(cfg.Data.Directory); e == nil && inside(dataRoot, resolved) {
			sensitive = true
		}
	}
	return sensitive
}

type screenCredentialMasker struct {
	raw       []byte
	positions []int
	text      string
}

func (m screenCredentialMasker) mask(start, count int) {
	for _, pos := range m.positions[start : start+count] {
		m.raw[pos] = '*'
	}
}
func (m screenCredentialMasker) hide(secret string, running bool) {
	needle := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, secret)
	if needle == "" {
		return
	}
	text := m.text
	for offset := 0; offset < len(text); {
		at := strings.Index(text[offset:], needle)
		if at < 0 {
			break
		}
		at += offset
		m.mask(at, len(needle))
		offset = at + len(needle)
	}
	if running {
		for n := min(len(needle)-1, len(text)); n > 0; n-- {
			if strings.HasSuffix(text, needle[:n]) {
				m.mask(len(text)-n, n)
				break
			}
		}
	}
}
