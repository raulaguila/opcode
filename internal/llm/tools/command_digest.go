package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Every bash result is compacted before it reaches the model, like a filtering
// shell proxy: the approved command is neither changed nor re-run, and the full
// output stays in an artifact. Errors, failures and panics are never matched by a noise pattern.
type digestRule struct {
	command, noise *regexp.Regexp
	rewrite        func(string) string // optional, for one line at a time
	reshape        func(string) string // optional, for the whole text; returns it as is when the format is unknown
}

var (
	digestRules = []digestRule{
		{command: regexp.MustCompile(`^go\s+test\b`), noise: regexp.MustCompile(`^(ok\s+\S+\s|\?\s+\S+\s+\[no test files\]|=== (RUN|PAUSE|CONT|NAME)\b|\s*--- (PASS|SKIP):|PASS$|go: (downloading|finding|extracting) )`), reshape: summarizeGoTest},
		{command: regexp.MustCompile(`^go\s`), noise: regexp.MustCompile(`^go: (downloading|finding|extracting) `)},
		{command: regexp.MustCompile(`^cargo\s`), noise: regexp.MustCompile(`^(\s*(Compiling|Downloading|Downloaded|Updating|Blocking|Checking|Finished|Fresh|Running|Locking)\s|test \S.* \.\.\. ok$|running \d+ tests?$)`)},
		{command: regexp.MustCompile(`^(python3?\s+-m\s+)?pytest\b`), noise: regexp.MustCompile(`^(\S+::\S.* PASSED\b|\S+\.py \.+\s*(\[\s*\d+%\])?$)`)},
		{command: regexp.MustCompile(`^((npm|yarn|pnpm)\s+(run\s+)?test|(npx\s+)?(jest|vitest))\b`), noise: regexp.MustCompile(`^\s*(PASS|✓|√|✔)\s`)},
		{command: regexp.MustCompile(`^(npm|pnpm|yarn)\s`), noise: regexp.MustCompile(`^(npm (warn|notice)\s|\s*Progress:|Packages: |\++$)`)},
		{command: regexp.MustCompile(`^(python3?\s+-m\s+)?pip3?\s`), noise: regexp.MustCompile(`^\s*(Collecting|Downloading|Using cached|Requirement already satisfied|Looking in|Installing collected|Preparing metadata|Getting requirements|Building wheels?|Attempting uninstall|Found existing|Uninstalling)\b`)},
		{command: regexp.MustCompile(`^git\s+(-C\s+\S+\s+|--no-pager\s+)*status\b`), noise: regexp.MustCompile(`^(\s*$|\s*\((use "git|create/copy|commit or discard)|no changes added to commit|nothing added to commit|Your branch is up to date)`), rewrite: compactGitStatus},
		{command: regexp.MustCompile(`^git\s+(-C\s+\S+\s+|--no-pager\s+)*log\b`), reshape: compactGitLog},
		{command: regexp.MustCompile(`^git\s+(-C\s+\S+\s+|--no-pager\s+)*(diff|show)\b`), reshape: compactDiff},
		{command: regexp.MustCompile(`^(find|fd|fdfind|rg\s+--files|git\s+(-C\s+\S+\s+|--no-pager\s+)*ls-files)\b`), reshape: compactPaths},
		{command: regexp.MustCompile(`^(e?grep|fgrep|rg|git\s+(-C\s+\S+\s+|--no-pager\s+)*grep|(npx\s+)?(tsc|eslint)|ruff|mypy|flake8|pylint|golangci-lint)\b`), reshape: compactLocations},
		// Anchored at column 0: diff context, commit bodies and file lists are indented.
		{command: regexp.MustCompile(`^git\s`), noise: regexp.MustCompile(`^( (create|delete) mode \d+ |(remote: )?(Enumerating|Counting|Compressing|Writing|Receiving|Resolving|Unpacking|Updating files|Delta compression|Total \d))`)},
		{command: regexp.MustCompile(`^(docker(\s+compose)?\s+logs|kubectl\s+logs|journalctl)\b`), reshape: collapseLogRepeats},
		{command: regexp.MustCompile(`^((docker|podman)\s+(ps|images|stats|compose\s+ps)|kubectl\s+(get|top)|df|du|free|ps|psql|wc|lsblk)\b`), noise: regexp.MustCompile(`^-+(\+-+)+$`), rewrite: collapseColumns},
		{command: regexp.MustCompile(`^docker\s`), noise: regexp.MustCompile(`^([0-9a-f]{12}: (Pulling|Waiting|Downloading|Extracting|Verifying|Pull complete|Download complete|Already exists)|#\d+ (sha256|CACHED|DONE|extracting|transferring)|Digest: sha256)`)},
		{command: regexp.MustCompile(`^(make|gmake)\b`), noise: regexp.MustCompile(`^g?make(\[\d+\])?: (Entering|Leaving|Nothing to be done)`)},
		{command: regexp.MustCompile(`^(apt|apt-get|dnf|yum)\s`), noise: regexp.MustCompile(`^(Get:|Hit:|Ign:|Reading |Building |Selecting |Preparing |Unpacking |Setting up |Processing |Fetched |WARNING: apt)`)},
		{command: regexp.MustCompile(`^(gradle|\./gradlew)\b`), noise: regexp.MustCompile(`^(> Task \S+( (UP-TO-DATE|NO-SOURCE|FROM-CACHE|SKIPPED))?$|Starting a Gradle Daemon|Download(ing)? https?:)`)},
		{command: regexp.MustCompile(`^(mvn|\./mvnw)\b`), noise: regexp.MustCompile(`^\[INFO\] (Download(ing|ed)( from)? |Scanning |Building |--- |-+$|$)`)},
		{command: regexp.MustCompile(`^wget\b`), noise: regexp.MustCompile(`^(Resolving |Connecting to |HTTP request sent|Length: |Saving to: |\s*\d+[KMG]?\s+[. ]+\s*\d+%)`)},
		{command: regexp.MustCompile(`^ls\s.*-\w*l`), noise: regexp.MustCompile(`^total \d+`), rewrite: compactListing},
	}
	digestSegments = regexp.MustCompile(`&&|;`)
	digestPrefix   = regexp.MustCompile(`^\s*(sudo\s+|\w+=\S*\s+|timeout\s+\S+\s+)*`)
	// These flags ask for the very lines the rules drop.
	digestRawFlags = regexp.MustCompile(`\s(-cover\S*|-bench\S*|-json|--format\S*|--json|--verbose|--message-format\S*)(\s|$)`)
	// The model asked for this content itself, so it is not cut by length.
	digestWhole     = regexp.MustCompile(`^(cat|sed|head|tail|awk|jq|git\s+(diff|show))\b`)
	digestImportant = regexp.MustCompile(`(?i)\b(error|errors|fail|failed|failure|fatal|panic|exception|traceback|denied|warning)\b`)
	listingLine     = regexp.MustCompile(`^([-dl])[rwxstST-]{9}[.+@]?\s+\d+\s+\S+\s+\S+\s+(\S+)\s+\S+\s+\S+\s+\S+\s+(.+)$`)
	gitStatusEntry  = regexp.MustCompile(`^\t(modified|new file|deleted|renamed):\s+(.+)$`)
	gitLogCommit    = regexp.MustCompile(`^commit ([0-9a-f]{40,64})( \(.+\))?$`)
	locationRow     = regexp.MustCompile(`^([^:()]+)(?::(\d+(?::\d+)?):|\((\d+,\d+)\):)(.*)$`) // path:line: and tsc's path(line,col):
	logTimestamp    = regexp.MustCompile(`^(\w{3} +\d+ )?\S*\d\d:\d\d:\d\d\S*\s+`)
	tableGap        = regexp.MustCompile(` {2,}`)
)

const (
	digestMinimumSaving = 512 // bytes; the note and artifact reference cost about this
	digestMaxLines      = 200
	digestHead          = 80
	digestTail          = 60
	digestMaxImportant  = 60
	digestMaxLine       = 2000 // bytes; minified bundles and base64 fit in one line
	digestMaxLocation   = 300  // bytes of text per grep or linter row
)

// digestCommandOutput compacts one result; ok is false when the saving would
// not pay for itself and the output should stay untouched.
func digestCommandOutput(command, stdout, stderr string) (string, string, bool) {
	rule, whole := digestRuleFor(command)
	out, errs := digestStream(stdout, rule, whole), digestStream(stderr, rule, whole)
	if len(stdout)+len(stderr)-len(out)-len(errs) < digestMinimumSaving {
		return stdout, stderr, false
	}
	return out, errs, true
}

func digestRuleFor(command string) (*digestRule, bool) {
	segments := digestSegments.Split(command, -1)
	last := digestPrefix.ReplaceAllString(segments[len(segments)-1], "")
	whole := digestWhole.MatchString(last)
	if strings.Contains(command, "|") || digestRawFlags.MatchString(command) {
		return nil, whole // a pipe chose its own shape
	}
	for i := range digestRules {
		if digestRules[i].command.MatchString(last) {
			return &digestRules[i], whole
		}
	}
	return nil, whole
}

func digestStream(text string, rule *digestRule, whole bool) string {
	var kept []string
	repeats := 0
	flush := func() {
		if repeats > 0 {
			kept[len(kept)-1] += fmt.Sprintf(" (×%d)", repeats+1)
		}
		repeats = 0
	}
	text = ansi.Strip(text)
	if !whole {
		if compact, ok := compactJSON(text); ok {
			return compact
		}
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		// A progress bar rewrites its line; only the last state matters.
		lines[i] = strings.TrimRight(line[strings.LastIndexByte(strings.TrimRight(line, "\r"), '\r')+1:], " \t\r")
		if !whole {
			lines[i] = capLine(lines[i], digestMaxLine)
		}
	}
	text = strings.Join(lines, "\n")
	if rule != nil && rule.reshape != nil {
		text = rule.reshape(text)
	}
	for line := range strings.SplitSeq(text, "\n") {
		if rule != nil {
			if rule.noise != nil && rule.noise.MatchString(line) {
				continue
			}
			if rule.rewrite != nil {
				line = rule.rewrite(line)
			}
		}
		if len(kept) > 0 && line == kept[len(kept)-1] {
			if line != "" {
				repeats++
			}
			continue
		}
		flush()
		kept = append(kept, line)
	}
	flush()
	if !whole {
		kept = digestLength(kept)
	}
	return strings.Trim(strings.Join(kept, "\n"), "\n") // the first row may be " M file"
}

// digestLength keeps both ends of a long output and the middle's error lines.
func digestLength(lines []string) []string {
	if len(lines) <= digestMaxLines {
		return lines
	}
	middle := lines[digestHead : len(lines)-digestTail]
	var important []string
	for _, line := range middle {
		if len(important) < digestMaxImportant && digestImportant.MatchString(line) {
			important = append(important, line)
		}
	}
	out := append([]string{}, lines[:digestHead]...)
	out = append(out, fmt.Sprintf("… %d lines omitted …", len(middle)-len(important)))
	out = append(out, important...)
	if len(important) > 0 {
		out = append(out, "…")
	}
	return append(out, lines[len(lines)-digestTail:]...)
}

// compactListing reduces an ls -l row to its name and size.
func compactListing(line string) string {
	match := listingLine.FindStringSubmatch(line)
	if match == nil {
		return line
	}
	switch match[1] {
	case "d":
		return match[3] + "/"
	case "l":
		return match[3]
	}
	return match[3] + " " + match[2]
}

// compactGitStatus keeps each section and shortens its rows to a letter and path.
func compactGitStatus(line string) string {
	switch line {
	case "Changes to be committed:":
		return "staged:"
	case "Changes not staged for commit:":
		return "unstaged:"
	case "Untracked files:":
		return "untracked:"
	}
	if match := gitStatusEntry.FindStringSubmatch(line); match != nil {
		return map[string]string{"modified": "M ", "new file": "A ", "deleted": "D ", "renamed": "R "}[match[1]] + match[2]
	}
	return strings.TrimPrefix(line, "\t")
}

// compactGitLog gives each commit one line with its subject; the body stays in
// the artifact. Patches, stats and custom formats are returned as they came.
func compactGitLog(text string) string {
	var out []string
	var author string
	subject := false
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		commit := gitLogCommit.FindStringSubmatch(line)
		name, isAuthor := strings.CutPrefix(line, "Author: ")
		date, isDate := strings.CutPrefix(line, "Date: ")
		switch {
		case commit != nil:
			out, subject = append(out, commit[1][:10]+commit[2]), false
		case len(out) == 0:
			return compactDiff(text)
		case isAuthor:
			author, _, _ = strings.Cut(name, " <")
		case isDate:
			date = strings.TrimSpace(date)
			if parsed, err := time.Parse("Mon Jan 2 15:04:05 2006 -0700", date); err == nil {
				date = parsed.Format("2006-01-02")
			}
			out[len(out)-1] += " " + date + " " + author + ":"
		case line == "", strings.HasPrefix(line, "Merge: "):
		case !strings.HasPrefix(line, "    "):
			return compactDiff(text)
		case !subject:
			out[len(out)-1] += " " + strings.TrimSpace(line)
			subject = true
		}
	}
	return strings.Join(out, "\n")
}

// compactLocations groups grep and linter rows under their file. Other rows
// stay and end the group, and a result that the length cut would split stays
// flat, so no row can lose its file.
func compactLocations(text string) string {
	var out []string
	file := ""
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		match := locationRow.FindStringSubmatch(line)
		if match == nil {
			out, file = append(out, line), ""
			continue
		}
		if match[1] != file {
			file = match[1]
			out = append(out, file+":")
		}
		out = append(out, match[2]+match[3]+": "+capLine(strings.TrimSpace(match[4]), digestMaxLocation))
	}
	if len(out) > digestMaxLines {
		return text
	}
	return strings.Join(out, "\n")
}

// compactPaths lists files per directory, so a long search keeps every name
// where the length cut would drop most. A name with a space stays flat.
func compactPaths(text string) string {
	var order []string
	names := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		if line == "" || strings.ContainsAny(line, " \t") {
			return text
		}
		cut := strings.LastIndexByte(line, '/') + 1
		dir := line[:cut]
		if _, seen := names[dir]; !seen {
			order = append(order, dir)
		}
		names[dir] = append(names[dir], line[cut:])
	}
	out := make([]string, 0, len(order))
	for _, dir := range order {
		out = append(out, strings.TrimSpace(dir+" "+strings.Join(names[dir], " ")))
	}
	return strings.Join(out, "\n")
}

// compactDiff drops the per-file headers that repeat the diff --git line.
func compactDiff(text string) string {
	var out []string
	header := false
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			header = true
		case strings.HasPrefix(line, "@@"):
			header = false
		case header && (strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ")):
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// compactJSON removes the indentation of a whole JSON document without
// changing a value; the streams of -json flags are not one document.
func compactJSON(text string) (string, bool) {
	trimmed := bytes.TrimSpace([]byte(text))
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return "", false
	}
	var out bytes.Buffer
	if json.Compact(&out, trimmed) != nil {
		return "", false
	}
	return out.String(), true
}

// collapseLogRepeats makes rows that differ only by their timestamp equal, so
// the repeat count in digestStream folds them.
func collapseLogRepeats(text string) string {
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		if logTimestamp.ReplaceAllString(lines[i], "") == logTimestamp.ReplaceAllString(lines[i-1], "") {
			lines[i] = lines[i-1]
		}
	}
	return strings.Join(lines, "\n")
}

func collapseColumns(line string) string {
	return tableGap.ReplaceAllString(strings.TrimLeft(line, " "), "  ")
}

// capLine keeps the start of an oversized line, on a character boundary.
func capLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return fmt.Sprintf("%s… +%d bytes", line[:cut], len(line)-cut)
}

// summarizeGoTest adds the count that survives dropping the passing rows.
func summarizeGoTest(text string) string {
	passed, failed, empty := 0, 0, 0
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(line, "ok ") && strings.Contains(line, "[no tests to run]"), strings.HasPrefix(line, "? "):
			empty++
		case strings.HasPrefix(line, "ok "):
			passed++
		case strings.HasPrefix(line, "FAIL\t"):
			failed++
		}
	}
	if passed+failed+empty == 0 {
		return text
	}
	return strings.TrimRight(text, "\n") + fmt.Sprintf("\n%d packages passed, %d failed, %d without tests\n", passed, failed, empty)
}
