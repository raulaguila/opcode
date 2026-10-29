package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func retrievalFixture(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(text), 0600))
}

type filePage struct {
	Files []string
	Total int
	More  bool
	Next  *int `json:"next_offset"`
}

func listPage(t *testing.T, tool BaseTool, input map[string]any) filePage {
	t.Helper()
	response, err := tool.Run(toolContext(), toolInput(input))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	var page filePage
	require.NoError(t, json.Unmarshal([]byte(response.Content), &page))
	require.Less(t, len(response.Content), 8500)
	return page
}

func TestListFilesPaginationFiltersAndPrivacy(t *testing.T) {
	root := productivityV2Workspace(t)
	testGit(t, root, "init", "-q")
	var want []string
	for i := range 137 {
		name := fmt.Sprintf("src/unit_%03d.go", i)
		retrievalFixture(t, root, name, "package demo\n")
		want = append(want, name)
	}
	for name, text := range map[string]string{".gitignore": "ignored.go\n", "ignored.go": "hidden", ".env": "secret", "node_modules/dependency.js": "dependency", ".opcode/private.txt": "state", "src/helper.py": "def run(): pass\n"} {
		retrievalFixture(t, root, name, text)
	}
	outside := filepath.Join(t.TempDir(), "external.go")
	require.NoError(t, os.WriteFile(outside, []byte("external"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "linked.go")))
	tool := SecureTool(NewLsTool(), &denyingPermissions{})
	offset := 0
	var found []string
	for {
		page := listPage(t, tool, map[string]any{"path": ".", "extension": "go", "offset": offset})
		require.LessOrEqual(t, len(page.Files), 60)
		require.Equal(t, len(want), page.Total)
		found = append(found, page.Files...)
		if !page.More {
			require.Nil(t, page.Next)
			break
		}
		require.NotNil(t, page.Next)
		require.Greater(t, *page.Next, offset)
		offset = *page.Next
	}
	require.Equal(t, want, found)
	page := listPage(t, tool, map[string]any{"path": "src", "language": "py", "glob": "*.py", "limit": 1})
	require.Equal(t, []string{"helper.py"}, page.Files)
	page = listPage(t, tool, map[string]any{"path": ".", "glob": "**/*.go", "ignore": []string{"unit_00*.go"}})
	for _, name := range page.Files {
		require.NotContains(t, name, "unit_00")
	}
	_, err := tool.Run(toolContext(), toolInput(map[string]any{"path": t.TempDir()}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
}

func TestListFilesByteBoundAndInvalidPages(t *testing.T) {
	root := productivityV2Workspace(t)
	for i := range 70 {
		retrievalFixture(t, root, fmt.Sprintf("%03d_%s.go", i, strings.Repeat("a", 160)), "package demo\n")
	}
	tool := SecureTool(NewLsTool(), nil)
	page := listPage(t, tool, map[string]any{"path": "."})
	require.Less(t, len(page.Files), 60)
	require.True(t, page.More)
	second := listPage(t, tool, map[string]any{"path": ".", "offset": *page.Next})
	require.NotEqual(t, page.Files[0], second.Files[0])
	require.Equal(t, 70, second.Total)
	for _, input := range []map[string]any{{"limit": 61}, {"offset": -1}, {"glob": "["}, {"extension": "../go"}} {
		response, err := tool.Run(toolContext(), toolInput(input))
		require.NoError(t, err)
		require.True(t, response.IsError)
	}
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	_, err := tool.Run(ctx, toolInput(map[string]any{"path": "."}))
	require.ErrorIs(t, err, context.Canceled)
}

type indexedResult struct {
	Path, Symbol, Preview, Signature, Language, Kind string
	Start                                            int `json:"start_line"`
	End                                              int `json:"end_line"`
}
type indexedPage struct {
	Results []indexedResult
	More    bool
	Reused  int `json:"reused_files"`
	Reads   int `json:"scan_reads"`
	Updated int `json:"updated_files"`
}

func searchPage(t *testing.T, tool BaseTool, input map[string]any) indexedPage {
	t.Helper()
	response, err := tool.Run(toolContext(), toolInput(input))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	var page indexedPage
	require.NoError(t, json.Unmarshal([]byte(response.Content), &page))
	require.Less(t, len(response.Content), 12500)
	for _, hit := range page.Results {
		require.LessOrEqual(t, len(hit.Preview), 240)
		require.LessOrEqual(t, len(hit.Signature), 160)
	}
	return page
}

func TestRankedCodeSearchScopesSymbolsAndBoundedPreviews(t *testing.T) {
	root := productivityV2Workspace(t)
	for i := range 10 {
		retrievalFixture(t, root, fmt.Sprintf("other/worker_%02d.go", i), "package demo\n// authentication permission\nfunc OtherWorker() {}\n")
	}
	retrievalFixture(t, root, "api/auth.go", "package demo\ntype Auth struct {}\n// authenticate users\nfunc (a *Auth) AuthenticateUser() bool {\n return true\n}\n")
	retrievalFixture(t, root, "api/auth.py", "def authenticate_user():\n    return True\n")
	retrievalFixture(t, root, "api-other/outside.go", "package demo\nfunc AuthenticateUser() {}\n")
	tool := SecureTool(NewCodeIndexTool(), nil)
	page := searchPage(t, tool, map[string]any{"query": "authentication permission"})
	require.Len(t, page.Results, 6)
	require.True(t, page.More)
	page = searchPage(t, tool, map[string]any{"query": "Auth.AuthenticateUser", "path": "api", "language": "golang", "kind": "method"})
	require.Len(t, page.Results, 1)
	require.Equal(t, "Auth.AuthenticateUser", page.Results[0].Symbol)
	require.Equal(t, "api/auth.go", page.Results[0].Path)
	page = searchPage(t, tool, map[string]any{"query": "authenticate user", "path": "api", "glob": "**/*.py", "language": "py", "extension": "py"})
	require.Len(t, page.Results, 1)
	require.Equal(t, "python", page.Results[0].Language)
	require.Equal(t, "function", page.Results[0].Kind)
	page = searchPage(t, tool, map[string]any{"query": "AuthenticateUser", "path": "api", "extension": "go"})
	for _, hit := range page.Results {
		require.NotContains(t, hit.Path, "api-other")
	}
	// Exact identifiers rank before overlapping general windows and partial names.
	retrievalFixture(t, root, "exact.go", "package demo\nfunc AuthenticateUser() {}\nfunc AuthenticateUserAlternative() {}\n")
	page = searchPage(t, tool, map[string]any{"query": "AuthenticateUser", "limit": 1})
	require.Equal(t, "AuthenticateUser", page.Results[0].Symbol)
}

func TestCodeIndexReusesMetadataExpiresAndDetectsSameSizeRewrite(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "main.go", "package demo\nfunc OldName() {}\n")
	tool := SecureTool(NewCodeIndexTool(), nil)
	first := searchPage(t, tool, map[string]any{"query": "OldName"})
	require.Equal(t, 1, first.Reads)
	cached := searchPage(t, tool, map[string]any{"query": "OldName"})
	require.Equal(t, 0, cached.Reads)
	require.Equal(t, 1, cached.Reused)
	path := filepath.Join(root, "main.go")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("package demo\nfunc NewName() {}\n"), 0600))
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))
	changed := searchPage(t, tool, map[string]any{"query": "NewName"})
	require.NotEmpty(t, changed.Results)
	require.Equal(t, 1, changed.Reads)
	old := searchPage(t, tool, map[string]any{"query": "OldName"})
	for _, hit := range old.Results {
		require.NotContains(t, hit.Preview, "OldName")
	}
	db, err := openCodeIndex(toolContext(), root)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE files SET verified=?`, time.Now().Add(-time.Minute*2).Unix())
	require.NoError(t, err)
	require.NoError(t, db.Close())
	expired := searchPage(t, tool, map[string]any{"query": "NewName"})
	require.Equal(t, 1, expired.Reads)
}

func TestCodeIndexCredentialChangesInvalidateCacheAndRetainLegacyIndex(t *testing.T) {
	root := productivityV2Workspace(t)
	secret := "credential-detected-after-first-index"
	retrievalFixture(t, root, "main.go", "package demo\n// marker "+secret+"\n")
	storage := filepath.Join(config.Get().Data.Directory, "code-index")
	require.NoError(t, os.MkdirAll(storage, 0700))
	legacy := filepath.Join(storage, indexDigest(root)+".sqlite")
	require.NoError(t, os.WriteFile(legacy, []byte("legacy-cache-marker"), 0600))
	tool := SecureTool(NewCodeIndexTool(), nil)
	searchPage(t, tool, map[string]any{"query": "marker"})
	t.Setenv("OPCODE_RETRIEVAL_API_KEY", secret)
	page := searchPage(t, tool, map[string]any{"query": "marker"})
	require.Equal(t, 1, page.Reads)
	for _, hit := range page.Results {
		require.NotContains(t, hit.Preview, secret)
	}
	db, err := openCodeIndex(toolContext(), root)
	require.NoError(t, err)
	defer db.Close()
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM chunks WHERE content LIKE ?`, "%"+secret+"%").Scan(&count))
	require.Zero(t, count)
	raw, err := os.ReadFile(legacy)
	require.NoError(t, err)
	require.Equal(t, "legacy-cache-marker", string(raw))
}

func TestMixedLanguageFiltersAndTextFallback(t *testing.T) {
	root := productivityV2Workspace(t)
	fixtures := map[string]struct{ Text, Language string }{
		"main.go": {"package demo\nfunc Needle() {}\n", "go"}, "main.py": {"def needle(): pass\n", "python"}, "main.ts": {"function needle() {}\n", "typescript"}, "main.tsx": {"function Needle() { return <div/> }\n", "tsx"}, "main.js": {"function needle() {}\n", "javascript"}, "main.rs": {"fn needle() {}\n", "rust"}, "main.java": {"class Needle {}\n", "java"}, "main.cpp": {"void needle() {}\n", "cpp"}, "main.cs": {"class Needle {}\n", "csharp"}, "main.kt": {"fun needle() {}\n", "kotlin"}, "main.sql": {"SELECT 'needle';\n", "sql"}, "main.nix": {"{ needle = 37; }\n", "nix"}, "main.xyz": {"needle unknown language\n", "text"},
	}
	for path, fixture := range fixtures {
		retrievalFixture(t, root, path, fixture.Text)
	}
	tool := SecureTool(NewCodeIndexTool(), nil)
	names := make([]string, 0, len(fixtures))
	for name := range fixtures {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fixture := fixtures[name]
		t.Run(name, func(t *testing.T) {
			page := searchPage(t, tool, map[string]any{"query": "needle", "language": fixture.Language})
			require.NotEmpty(t, page.Results)
			for _, hit := range page.Results {
				require.Equal(t, fixture.Language, hit.Language)
				expected := map[string]string{"go": "function", "python": "function", "javascript": "function", "typescript": "function", "tsx": "function", "rust": "function", "cpp": "function", "csharp": "class", "java": "class", "kotlin": "function"}[hit.Language]
				if expected == "" {
					expected = "text"
				}
				require.Equal(t, expected, hit.Kind)
			}
		})
	}
}

func TestExpandedParserPolicyRebuildsUnchangedLegacyChunks(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "ledger.rb", "class Ledger\n def needle\n  37\n end\nend\n")
	tool := SecureTool(NewCodeIndexTool(), nil)
	page := searchPage(t, tool, map[string]any{"query": "needle", "language": "ruby"})
	require.NotEmpty(t, page.Results)
	db, err := openCodeIndex(toolContext(), root)
	require.NoError(t, err)
	secrets := credentialValues()
	sort.Strings(secrets)
	legacy := indexDigest("parser-v4\x00" + strings.Join(secrets, "\x00"))
	_, err = db.Exec(`UPDATE files SET policy=?`, legacy)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE chunks SET symbol='<file>',kind='text'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	page = searchPage(t, tool, map[string]any{"query": "needle", "language": "ruby"})
	require.NotEmpty(t, page.Results)
	require.Equal(t, "Ledger.needle", page.Results[0].Symbol)
	require.Equal(t, "method", page.Results[0].Kind)
}
