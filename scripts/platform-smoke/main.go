// Command platform-smoke exercises a built native binary without credentials or inference.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func main() {
	binary := flag.String("binary", "bin/opcode", "Already-built native Opcode binary")
	flag.Parse()
	if err := smoke(*binary); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("PASS native CLI/config smoke %s/%s; no inference, PTY or real vault accessed\n", runtime.GOOS, runtime.GOARCH)
}
func smoke(binary string) error {
	binary, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "opcode-native-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(project, 0700); err != nil {
		return err
	}
	env := smokeEnvironment(home)
	run := smokeCommand(binary, project, env)
	if err := smokeInformational(run, home, project); err != nil {
		return err
	}
	if err := smokeTemplates(run, home, project); err != nil {
		return err
	}
	// A configured literal must stay out of the effective document export.
	private := filepath.Join(root, "candidate.json")
	const secret = "fake-platform-smoke-key-value"
	if err := os.WriteFile(private, []byte(`{"providers":{"openai":{"apiKey":"`+secret+`"}}}`), 0600); err != nil {
		return err
	}
	output, err := run("config", "show", "--cwd", project, "--file", private, "--redacted")
	if err != nil || bytes.Contains(output, []byte(secret)) {
		return fmt.Errorf("redacted config export failed or exposed fixture key")
	}
	return nil
}

// Do not inherit provider variables, discovery URLs, vault/session addresses or
// user config. Windows still needs its system locations to start native programs.
func smokeEnvironment(home string) []string {
	env := []string{}
	for _, name := range []string{"PATH", "SYSTEMROOT", "WINDIR", "PATHEXT", "COMSPEC", "TMPDIR", "TMP", "TEMP"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		env = append(env, name+"="+home)
	}
	return append(env, "NO_COLOR=1", "TERM=dumb", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
}

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 65536 - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, p[:min(remaining, len(p))]...)
	}
	return n, nil
}

func smokeCommand(binary, project string, env []string) func(...string) ([]byte, error) {
	return func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = env
		command.Dir = project
		var output boundedOutput
		command.Stdout = &output
		command.Stderr = &output
		err := command.Run()
		if ctx.Err() != nil {
			return output.data, fmt.Errorf("native command timed out")
		}
		return output.data, err
	}
}

func smokeInformational(run func(...string) ([]byte, error), home, project string) error {
	for _, args := range [][]string{{"--help"}, {"--version"}, {"config", "path", "--cwd", project}, {"config", "validate", "--cwd", project}} {
		output, err := run(args...)
		if err != nil {
			return fmt.Errorf("informational command %v failed: %s", args, output)
		}
		for _, name := range []string{".opcode", "opcode"} {
			if _, err := os.Stat(filepath.Join(home, name)); !os.IsNotExist(err) {
				return fmt.Errorf("informational command created user configuration")
			}
		}
	}
	return nil
}

func smokeTemplates(run func(...string) ([]byte, error), home, project string) error {
	startup := func() error { return smokeStartup(run, project) }
	if err := startup(); err != nil {
		return err
	}
	active, example := filepath.Join(home, ".opcode", "config.json"), filepath.Join(home, "opcode", "config.example.json")
	for _, path := range []string{active, example} {
		data, err := os.ReadFile(path)
		if err != nil || !json.Valid(data) {
			return fmt.Errorf("startup did not create a valid configuration/template")
		}
	}
	sentinel := []byte(`{"runtime":{"mode":"ask"},"future":"native-smoke-sentinel"}`)
	if err := os.WriteFile(active, sentinel, 0600); err != nil {
		return err
	}
	if err := os.Remove(example); err != nil {
		return err
	}
	if err := startup(); err != nil {
		return err
	}
	current, err := os.ReadFile(active)
	if err != nil || !bytes.Equal(current, sentinel) {
		return fmt.Errorf("second startup overwrote user settings")
	}
	data, err := os.ReadFile(example)
	if err != nil || !json.Valid(data) || bytes.Contains(data, []byte("native-smoke-sentinel")) {
		return fmt.Errorf("reference template was not independently restored")
	}
	for _, args := range [][]string{{"config", "validate", "--cwd", project}, {"config", "show", "--cwd", project, "--redacted"}} {
		output, err := run(args...)
		if err != nil {
			return fmt.Errorf("configuration command failed: %s", output)
		}
	}
	return nil
}

func smokeStartup(run func(...string) ([]byte, error), project string) error {
	output, err := run("--cwd", project, "--prompt", "fixture: must not reach inference")
	if err == nil || !bytes.Contains(output, []byte("no model configured")) {
		return fmt.Errorf("startup did not stop safely before inference: %s", output)
	}
	return nil
}
