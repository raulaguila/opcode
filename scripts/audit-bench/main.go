// Command audit-bench scores /auditoria against bugs planted in testdata/shop:
// how many it finds, and how much of what it says is not a bug.
//
//	go run ./scripts/audit-bench -bin ./opcode -config .opcode.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type bug struct {
	File       string `json:"file"`
	Code       string `json:"code"`
	Kind       string `json:"kind"`
	Occurrence int    `json:"occurrence"`
	line       int
}

type finding struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Problem  string `json:"problem"`
	Source   string `json:"source"`
	State    string `json:"state"`
}

// reach is how far a finding may sit from a bug's line: a model often names the
// line of the function or of the call just before it.
const reach = 2

type score struct {
	found, missed []bug
	wrong         []finding
	right         int
}

func grade(bugs []bug, findings []finding) score {
	var result score
	matched := make([]bool, len(bugs))
	for _, item := range findings {
		hit := false
		for i, planted := range bugs {
			if planted.File == item.Path && item.Line >= planted.line-reach && item.Line <= planted.line+reach {
				matched[i], hit = true, true
			}
		}
		if hit {
			result.right++
		} else {
			result.wrong = append(result.wrong, item)
		}
	}
	for i, planted := range bugs {
		if matched[i] {
			result.found = append(result.found, planted)
		} else {
			result.missed = append(result.missed, planted)
		}
	}
	return result
}

func locate(root string, bugs []bug) error {
	for i := range bugs {
		data, err := os.ReadFile(filepath.Join(root, bugs[i].File))
		if err != nil {
			return err
		}
		seen := 0
		for number, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, bugs[i].Code) {
				if seen == bugs[i].Occurrence {
					bugs[i].line = number + 1
					break
				}
				seen++
			}
		}
		if bugs[i].line == 0 {
			return fmt.Errorf("%s: %q is not in the fixture", bugs[i].File, bugs[i].Code)
		}
	}
	return nil
}

func run() error {
	binary := flag.String("bin", "opcode", "opcode binary")
	configuration := flag.String("config", ".opcode.json", "configuration with the model to score")
	fixture := flag.String("fixture", "scripts/audit-bench/testdata", "folder with bugs.json and shop")
	verbose := flag.Bool("v", false, "list what was missed and what was wrong")
	flag.Parse()

	raw, err := os.ReadFile(filepath.Join(*fixture, "bugs.json"))
	if err != nil {
		return err
	}
	var bugs []bug
	if err := json.Unmarshal(raw, &bugs); err != nil {
		return err
	}
	if err := locate(filepath.Join(*fixture, "shop"), bugs); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "audit-bench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := os.CopyFS(work, os.DirFS(filepath.Join(*fixture, "shop"))); err != nil {
		return err
	}
	settings, err := os.ReadFile(*configuration)
	if err != nil {
		return err
	}
	// Provider settings count only in the global file, so the run gets its own home.
	home, err := os.MkdirTemp("", "audit-bench-home-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(home)
	if err := os.Mkdir(filepath.Join(home, ".opcode"), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, ".opcode", "config.json"), settings, 0o600); err != nil {
		return err
	}
	if output, err := exec.Command("git", "init", "-q", work).CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w: %s", err, output)
	}
	absolute, err := filepath.Abs(*binary)
	if err != nil {
		return err
	}
	started := time.Now()
	command := exec.Command(absolute, "-p", "/auditoria", "-q", "-f", "json", "--approval", "sandbox", "-c", ".")
	command.Dir = work
	command.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"))
	output, err := command.Output()
	if _, partial := err.(*exec.ExitError); err != nil && !partial {
		return err
	}
	data, err := os.ReadFile(filepath.Join(work, ".opcode", "audit", "findings.json"))
	if err != nil {
		return fmt.Errorf("no findings: %w: %s", err, output)
	}
	var findings []finding
	if err := json.Unmarshal(data, &findings); err != nil {
		return err
	}
	result := grade(bugs, findings)
	precision := 0.0
	if len(findings) > 0 {
		precision = float64(result.right) / float64(len(findings))
	}
	fmt.Printf("found %d/%d bugs · %d findings, %.0f%% on a bug · %s\n", len(result.found), len(bugs), len(findings), 100*precision, time.Since(started).Round(time.Second))
	kinds := map[string][2]int{}
	for _, planted := range result.found {
		count := kinds[planted.Kind]
		count[0]++
		kinds[planted.Kind] = count
	}
	for _, planted := range result.missed {
		count := kinds[planted.Kind]
		count[1]++
		kinds[planted.Kind] = count
	}
	names := make([]string, 0, len(kinds))
	for name := range kinds {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		fmt.Printf("  %-12s %d/%d\n", name, kinds[name][0], kinds[name][0]+kinds[name][1])
	}
	if *verbose {
		for _, planted := range result.missed {
			fmt.Printf("missed %s:%d %s\n", planted.File, planted.line, planted.Code)
		}
		for _, item := range result.wrong {
			fmt.Printf("wrong  [%s %s %s] %s:%d %s\n", item.Severity, item.Source, item.State, item.Path, item.Line, item.Problem)
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "audit-bench:", err)
		os.Exit(1)
	}
}
