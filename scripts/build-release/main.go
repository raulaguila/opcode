// Command build-release creates deterministic local archives; it never publishes.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	version := flag.String("version", "dev", "Version embedded in the binary")
	output := flag.String("output", "dist/local", "Output directory")
	targets := flag.String("targets", "linux/amd64,linux/arm64,darwin/amd64,darwin/arm64", "Comma-separated OS/architecture pairs")
	flag.Parse()
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,63}$`).MatchString(*version) {
		return fmt.Errorf("invalid release version")
	}
	if err := os.MkdirAll(*output, 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(*output, ".opcode-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	checksums := []string{}
	seen := map[string]bool{}
	for _, target := range strings.Split(*targets, ",") {
		parts := strings.Split(target, "/")
		if len(parts) != 2 || (parts[0] != "linux" && parts[0] != "darwin") || (parts[1] != "amd64" && parts[1] != "arm64") || seen[target] {
			return fmt.Errorf("unsupported or duplicate target %q", target)
		}
		seen[target] = true
		binary := filepath.Join(staging, "opcode")
		command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -buildid= -X github.com/raulaguila/opcode/internal/version.Version="+*version, "-o", binary, ".")
		env := []string{}
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			switch name {
			case "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT", "GOAMD64", "GOARM64":
				continue
			}
			env = append(env, entry)
		}
		command.Env = append(env, "GOOS="+parts[0], "GOARCH="+parts[1], "CGO_ENABLED=0", "GOFLAGS=")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("build %s: %w", target, err)
		}
		name := "opcode-" + *version + "-" + parts[0] + "-" + parts[1] + ".tar.gz"
		path := filepath.Join(staging, name)
		if err := archive(path, binary); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil {
			return err
		}
		checksums = append(checksums, hex.EncodeToString(hash.Sum(nil))+"  "+name)
		if err := os.Rename(path, filepath.Join(*output, name)); err != nil {
			return err
		}
		fmt.Println(name)
	}
	sort.Slice(checksums, func(i, j int) bool { return strings.Fields(checksums[i])[1] < strings.Fields(checksums[j])[1] })
	return os.WriteFile(filepath.Join(*output, "checksums.txt"), []byte(strings.Join(checksums, "\n")+"\n"), 0644)
}
func archive(path, binary string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	gz.Header.ModTime = time.Unix(0, 0)
	gz.Header.OS = 255
	w := tar.NewWriter(gz)
	for _, entry := range []struct {
		name, path string
		mode       int64
	}{{"opcode", binary, 0755}, {"LICENSE", "LICENSE", 0644}, {"README.md", "README.md", 0644}} {
		input, err := os.Open(entry.path)
		if err != nil {
			return err
		}
		info, err := input.Stat()
		if err != nil {
			input.Close()
			return err
		}
		if err := w.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: info.Size(), ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(w, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return file.Close()
}
