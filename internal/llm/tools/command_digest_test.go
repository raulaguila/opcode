package tools

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func goTestOutput(extra string) string {
	var out strings.Builder
	for i := range 20 {
		fmt.Fprintf(&out, "ok  \texample.com/p%d\t0.1%ds\n", i, i%10)
		fmt.Fprintf(&out, "?   \texample.com/q%d\t[no test files]\n", i)
	}
	out.WriteString(extra)
	return out.String()
}

func TestDigestKeepsFailuresAndDropsPassingLines(t *testing.T) {
	failure := "--- FAIL: TestX (0.00s)\n    x_test.go:9: want 1\nFAIL\nFAIL\texample.com/x\t0.2s\npanic: boom"
	out, errs, ok := digestCommandOutput("go test ./...", goTestOutput(failure), "")
	require.True(t, ok)
	require.Equal(t, failure+"\n20 packages passed, 1 failed, 20 without tests", out)
	require.Empty(t, errs)

	verbose := strings.Repeat("=== RUN   TestAlpha\n--- PASS: TestAlpha (0.00s)\n", 20) + "=== RUN   TestB\n    b_test.go:1: nope\n--- FAIL: TestB (0.00s)\nFAIL\n"
	out, _, ok = digestCommandOutput("cd api && GOFLAGS=-v timeout 60 go test -v ./x", verbose, "")
	require.True(t, ok)
	require.Contains(t, out, "b_test.go:1: nope")
	require.Contains(t, out, "--- FAIL: TestB")
	require.NotContains(t, out, "TestAlpha")
}

func TestDigestRespectsAFormatTheCommandChose(t *testing.T) {
	raw := goTestOutput("")
	for _, command := range []string{"go test ./... | tail -5", "go test -cover ./...", "go test -json ./...", "echo go test"} {
		out, _, ok := digestCommandOutput(command, raw, "")
		require.False(t, ok, command)
		require.Equal(t, raw, out, command)
	}
	few := "ok  \texample.com/one\t0.1s\n"
	out, _, ok := digestCommandOutput("go test ./one", few, "")
	require.False(t, ok, "a short result keeps its own line")
	require.Equal(t, few, out)
}

func TestDigestCoversCommonTools(t *testing.T) {
	cargo := strings.Repeat("   Compiling crate v1.0.0\n", 20) + strings.Repeat("test a::ok ... ok\n", 20) + "test a::bad ... FAILED\n\ntest result: FAILED. 5 passed; 1 failed\n"
	_, errs, _ := digestCommandOutput("cargo test", "", cargo)
	require.Equal(t, "test a::bad ... FAILED\n\ntest result: FAILED. 5 passed; 1 failed", errs)

	status := "On branch main\nYour branch is up to date with 'origin/main'.\n\nChanges not staged for commit:\n  (use \"git add <file>...\" to update what will be committed)\n  (use \"git restore <file>...\" to discard changes in working directory)\n\tmodified:   a.go\n\n" + strings.Repeat("  (use \"git add <file>...\" to include in what will be committed)\n", 10)
	out, _, ok := digestCommandOutput("git status", status, "")
	require.True(t, ok)
	require.Equal(t, "On branch main\nunstaged:\nM a.go", out)

	listing := "total 8\n" + strings.Repeat("-rw-rw-r-- 1 raul raul 1234 Nov  3 19:51 main.go\ndrwxrwxr-x 2 raul raul 4096 Nov  3 19:51 internal\n", 10)
	out, _, ok = digestCommandOutput("ls -la", listing, "")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(out, "main.go 1234\ninternal/\n"))
}

func TestDigestWorksOnAnyCommand(t *testing.T) {
	progress := strings.Repeat("\x1b[32m10%\rworking 50%\rdone 100%\x1b[0m\n", 40)
	out, _, ok := digestCommandOutput("./build.sh", progress, "")
	require.True(t, ok)
	require.Equal(t, "done 100% (×40)", out, "colors, progress and repeats collapse")

	var long strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&long, "line %d\n", i)
		if i == 500 {
			long.WriteString("fatal: disk full\n")
		}
	}
	out, _, ok = digestCommandOutput("./build.sh", long.String(), "")
	require.True(t, ok)
	require.Contains(t, out, "line 0\n")
	require.Contains(t, out, "line 999")
	require.Contains(t, out, "fatal: disk full", "an error in the cut middle survives")
	require.NotContains(t, out, "line 500\n")
	require.Less(t, len(strings.Split(out, "\n")), 210)

	out, _, _ = digestCommandOutput("cat big.txt", long.String(), "")
	require.Contains(t, out, "line 500\n", "content asked for by name is not cut")
}

func TestDigestShortensGitAndGrep(t *testing.T) {
	var log strings.Builder
	for i := range 12 {
		fmt.Fprintf(&log, "commit %040d\nAuthor: Ana Lima <ana@example.com>\nDate:   Tue Nov 3 17:47:06 2026 +0000\n\n    fix: subject %d\n\n    body %d\n\n", i, i, i)
	}
	out, _, ok := digestCommandOutput("git log -12", log.String(), "")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(out, "0000000000 2026-11-03 Ana Lima: fix: subject 0\n0000000000 2026-11-03 Ana Lima: fix: subject 1\n"), out)
	require.NotContains(t, out, "body")

	patch := log.String() + "diff --git a/a.go b/a.go\n+added line\n"
	out, _, _ = digestCommandOutput("git log -p", patch, "")
	require.Contains(t, out, "+added line", "a patch keeps its original shape")
	require.Contains(t, out, "body 3")

	var hits strings.Builder
	for i := range 40 {
		fmt.Fprintf(&hits, "internal/some/long/path/file.go:%d:\t\tcall(%d)\n", i, i)
	}
	out, _, ok = digestCommandOutput("grep -rn call internal", hits.String(), "")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(out, "internal/some/long/path/file.go:\n0: call(0)\n1: call(1)\n"), out)

	hits.Reset()
	for i := range 300 {
		fmt.Fprintf(&hits, "internal/some/long/path/file.go:%d:call(%d)\n", i, i)
	}
	out, _, _ = digestCommandOutput("rg -n call", hits.String(), "")
	require.True(t, strings.HasSuffix(out, "internal/some/long/path/file.go:299:call(299)"), "a cut result keeps the file on every row")
}

func TestDigestKeepsLinesThatOnlyLookLikeGitProgress(t *testing.T) {
	diff := "index 1234567..89abcde 100644\n Writing to disk\n-Counting items\n" + strings.Repeat(" unchanged context\n", 1)
	out, _, _ := digestCommandOutput("git diff", diff+strings.Repeat("index 1234567..89abcde 100644\n", 40), "")
	require.Contains(t, out, " Writing to disk")

	status := "Untracked files:\n  (use \"git add <file>...\" to include in what will be committed)\n\tWriting.md\n\n" + strings.Repeat("\tmodified:   internal/some/long/path/file.go\n", 30)
	out, _, _ = digestCommandOutput("git -C /tmp/x status", status, "")
	require.Contains(t, out, "Writing.md")

	short := " M a.go\n" + strings.Repeat("?? internal/some/long/path/file.go\n", 30) + strings.Repeat("?? internal/some/long/path/other.go\n", 30)
	out, _, ok := digestCommandOutput("git status --short", short, "")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(out, " M a.go\n"), "the leading column says the change is unstaged")
}

func TestGoTestSummaryCountsPackagesThatRanNothing(t *testing.T) {
	out, _, ok := digestCommandOutput("go test -run TestTypo ./...", strings.Repeat("ok  \texample.com/a\t0.1s [no tests to run]\n", 30), "")
	require.True(t, ok)
	require.Equal(t, "0 packages passed, 0 failed, 30 without tests", out)
}

func TestDigestCompactsJSONWithoutChangingIt(t *testing.T) {
	var pretty strings.Builder
	pretty.WriteString("[\n")
	for i := range 80 {
		fmt.Fprintf(&pretty, "  {\n    \"id\": %d,\n    \"name\": \"a  b\"\n  },\n", i)
	}
	pretty.WriteString("  {}\n]\n")
	out, _, ok := digestCommandOutput("curl -s http://localhost/x", pretty.String(), "")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(out, `[{"id":0,"name":"a  b"},{"id":1,`), "spaces inside values stay")

	out, _, _ = digestCommandOutput("cat data.json", pretty.String(), "")
	require.Contains(t, out, "\n", "a file read by name keeps its text for edits")
}

func TestDigestGroupsPathsAndLocations(t *testing.T) {
	var paths strings.Builder
	for i := range 300 {
		fmt.Fprintf(&paths, "internal/some/long/path/file%d.go\n", i)
	}
	paths.WriteString("internal/top.go\nREADME.md\n")
	out, _, ok := digestCommandOutput("find . -name '*.go'", paths.String(), "")
	require.True(t, ok)
	lines := strings.Split(out, "\n")
	require.Len(t, lines, 3)
	require.True(t, strings.HasPrefix(lines[0], "internal/some/long/path/ file0.go file1.go"))
	require.True(t, strings.HasSuffix(lines[0], " file299.go"), "no name is lost")
	require.Equal(t, "internal/ top.go", lines[1])
	require.Equal(t, "README.md", lines[2])

	spaced := strings.Repeat("internal/my file.go\n", 40) + "internal/other.go\n"
	out, _, _ = digestCommandOutput("find .", spaced, "")
	require.Contains(t, out, "internal/my file.go", "a name with a space stays whole")

	lint := strings.Repeat("src/some/long/component/name.ts(12,5): error TS2322: Type mismatch\n", 1)
	for i := range 30 {
		lint += fmt.Sprintf("src/some/long/component/name.ts(%d,5): error TS2322: Type mismatch\n", i)
	}
	out, _, ok = digestCommandOutput("npx tsc --noEmit", lint+"Found 31 errors.\n", "")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(out, "src/some/long/component/name.ts:\n12,5: error TS2322: Type mismatch\n0,5: error"), out)
	require.True(t, strings.HasSuffix(out, "\nFound 31 errors."))

	rows := strings.Repeat("a/some/long/path/file.go:1:match\na/some/long/path/file.go-2-context\n--\n", 20)
	out, _, _ = digestCommandOutput("grep -rn -A1 match a", rows, "")
	require.Contains(t, out, "a/some/long/path/file.go-2-context", "context rows pass through")

	long := strings.Repeat("a/some/long/path/file.go:7:"+strings.Repeat("é", 400)+"\n", 3)
	out, _, ok = digestCommandOutput("grep -rn x a", long, "")
	require.True(t, ok)
	require.Contains(t, out, "… +")
	require.True(t, utf8.ValidString(out))
}

func TestDigestShortensDiffHeadersAndTables(t *testing.T) {
	file := "diff --git a/a.go b/a.go\nindex 1234567..89abcde 100644\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,2 @@\n context\n--- removed sql comment\n+++ added text\n"
	out, _, ok := digestCommandOutput("git diff", strings.Repeat(file, 20), "")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(out, "diff --git a/a.go b/a.go\n@@ -1,2 +1,2 @@\n context\n--- removed sql comment\n+++ added text\n"), out)

	table := "NAME" + strings.Repeat(" ", 30) + "STATUS" + strings.Repeat(" ", 30) + "AGE\n"
	table += strings.Repeat("web-1"+strings.Repeat(" ", 29)+"Running"+strings.Repeat(" ", 29)+"3d\n", 20)
	out, _, ok = digestCommandOutput("kubectl get pods", table, "")
	require.True(t, ok)
	require.Contains(t, out, "NAME  STATUS  AGE\nweb-1  Running  3d")

	psql := " id |  name\n----+--------\n" + strings.Repeat("  1 | Ana Lima"+strings.Repeat(" ", 40)+"\n", 20)
	out, _, ok = digestCommandOutput("psql -c 'select 1'", psql, "")
	require.True(t, ok)
	require.NotContains(t, out, "----+")
	require.Contains(t, out, "1 | Ana Lima")
}

func TestDigestFoldsLogsAndCapsLongLines(t *testing.T) {
	var logs strings.Builder
	for i := range 40 {
		fmt.Fprintf(&logs, "2026-11-03T20:37:%02dZ worker retry connection refused\n", i)
	}
	logs.WriteString("2026-11-03T20:38:01Z worker stopped\n")
	out, _, ok := digestCommandOutput("docker logs api", logs.String(), "")
	require.True(t, ok)
	require.Equal(t, "2026-11-03T20:37:00Z worker retry connection refused (×40)\n2026-11-03T20:38:01Z worker stopped", out)

	out, _, ok = digestCommandOutput("./bundle.sh", "start\n"+strings.Repeat("x", 9000)+"\nerror: end\n", "")
	require.True(t, ok)
	require.Contains(t, out, "x… +7000 bytes\nerror: end")
	out, _, _ = digestCommandOutput("cat app.min.js", strings.Repeat("x", 9000), "")
	require.Len(t, out, 9000, "content asked for by name keeps its length")
}
