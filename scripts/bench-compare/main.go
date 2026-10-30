// Command bench-compare gates allocation growth in paired component benchmarks.
// Elapsed time is reported, not gated across machines or noisy CI runners.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

type sample struct {
	NS     float64 `json:"ns_per_op"`
	Bytes  float64 `json:"bytes_per_op"`
	Allocs float64 `json:"allocs_per_op"`
}
type comparison struct {
	Name                 string `json:"name"`
	Before               sample `json:"before"`
	After                sample `json:"after"`
	AllocationRegression bool   `json:"allocation_regression"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("bench-compare", flag.ContinueOnError)
	flags.SetOutput(errOut)
	beforePath := flags.String("before", "", "Baseline Go benchmark log (at least 3 samples per case)")
	afterPath := flags.String("after", "", "Candidate Go benchmark log")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *beforePath == "" || *afterPath == "" || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "use -before baseline.log -after candidate.log")
		return 2
	}
	rows, err := compareFiles(*beforePath, *afterPath)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	if err := json.NewEncoder(out).Encode(rows); err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	for _, row := range rows {
		if row.AllocationRegression {
			return 1
		}
	}
	return 0
}
func compareFiles(beforePath, afterPath string) ([]comparison, error) {
	before, err := readFileSamples(beforePath)
	if err != nil {
		return nil, err
	}
	after, err := readFileSamples(afterPath)
	if err != nil {
		return nil, err
	}
	return compare(before, after)
}
func readFileSamples(path string) (map[string][]sample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readSamples(file)
}
func readSamples(input io.Reader) (map[string][]sample, error) {
	result := map[string][]sample{}
	pkg := ""
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "pkg: ") {
			pkg = strings.TrimSpace(strings.TrimPrefix(line, "pkg: "))
			continue
		}
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		} // The runner can print an incomplete progress prefix.
		name := benchmarkName(fields[0])
		value, err := parseSample(fields)
		if err != nil {
			return nil, fmt.Errorf("benchmark %s: %w", name, err)
		}
		result[pkg+"/"+name] = append(result[pkg+"/"+name], value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no complete benchmark samples")
	}
	return result, nil
}
func benchmarkName(name string) string {
	if dash := strings.LastIndex(name, "-"); dash >= 0 {
		if _, err := strconv.Atoi(name[dash+1:]); err == nil {
			return name[:dash]
		}
	}
	return name
}

func parseSample(fields []string) (sample, error) {
	var value sample
	found := 0
	for i := 2; i+1 < len(fields); i++ {
		var target *float64
		bit := 0
		switch fields[i+1] {
		case "ns/op":
			target = &value.NS
			bit = 1
		case "B/op":
			target = &value.Bytes
			bit = 2
		case "allocs/op":
			target = &value.Allocs
			bit = 4
		}
		if target == nil {
			continue
		}
		if found&bit != 0 {
			return value, fmt.Errorf("duplicate benchmark metric")
		}
		number, err := strconv.ParseFloat(fields[i], 64)
		if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
			return value, fmt.Errorf("invalid benchmark metric")
		}
		*target = number
		found |= bit
	}
	if found != 7 {
		return value, fmt.Errorf("missing ns/op, B/op or allocs/op; use -benchmem")
	}
	return value, nil
}
func compare(before, after map[string][]sample) ([]comparison, error) {
	for name := range after {
		if _, exists := before[name]; !exists {
			return nil, fmt.Errorf("%s has no baseline samples", name)
		}
	}
	keys := make([]string, 0, len(before))
	for key := range before {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]comparison, 0, len(keys))
	for _, name := range keys {
		left, right := before[name], after[name]
		if len(left) < 3 || len(right) < 3 {
			return nil, fmt.Errorf("%s requires at least 3 samples in both logs", name)
		}
		old, next := median(left), median(right)
		rows = append(rows, comparison{Name: name, Before: old, After: next, AllocationRegression: next.Bytes > old.Bytes*1.10 || next.Allocs > old.Allocs*1.10})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no comparable benchmark cases")
	}
	return rows, nil
}
func median(samples []sample) sample {
	times, bytes, allocs := make([]float64, len(samples)), make([]float64, len(samples)), make([]float64, len(samples))
	for i, value := range samples {
		times[i], bytes[i], allocs[i] = value.NS, value.Bytes, value.Allocs
	}
	return sample{NS: middle(times), Bytes: middle(bytes), Allocs: middle(allocs)}
}
func middle(values []float64) float64 {
	sort.Float64s(values)
	center := len(values) / 2
	if len(values)%2 == 0 {
		return (values[center-1] + values[center]) / 2
	}
	return values[center]
}
