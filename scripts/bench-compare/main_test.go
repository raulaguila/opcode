package main

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestParseAndCompareRetainsAllocationGate(t *testing.T) {
	baseline := "pkg: example/test\nBenchmarkCase-8 20 10 ns/op 2.5 MB/s 100 B/op 10 allocs/op\n"
	left, err := readSamples(strings.NewReader(strings.Repeat(baseline, 3)))
	require.NoError(t, err)
	right, err := readSamples(strings.NewReader(strings.Repeat(strings.ReplaceAll(baseline, "100 B/op", "120 B/op"), 3)))
	require.NoError(t, err)
	rows, err := compare(left, right)
	require.NoError(t, err)
	require.True(t, rows[0].AllocationRegression)
	right["example/test/BenchmarkCase"] = []sample{{NS: 1000, Bytes: 100, Allocs: 10}, {NS: 10, Bytes: 100, Allocs: 10}, {NS: 10, Bytes: 100, Allocs: 10}}
	rows, err = compare(left, right)
	require.NoError(t, err)
	require.False(t, rows[0].AllocationRegression)
	require.Equal(t, float64(10), rows[0].After.NS)
	_, err = compare(left, map[string][]sample{})
	require.ErrorContains(t, err, "at least 3 samples")
}
func TestBenchmarkInputCannotSilentlyPassMissingMetrics(t *testing.T) {
	for _, input := range []string{"", "BenchmarkCase-8 20 10 ns/op", "BenchmarkCase-8 20 -1 ns/op 0 B/op 0 allocs/op", "BenchmarkCase-8 20 NaN ns/op 0 B/op 0 allocs/op", "BenchmarkCase-8 20 1 ns/op 0 ns/op 0 allocs/op"} {
		_, err := readSamples(strings.NewReader(input))
		require.Error(t, err)
	}
	before := map[string][]sample{"zero": {{NS: 1}, {NS: 1}, {NS: 1}}}
	after := map[string][]sample{"zero": {{NS: 1, Bytes: 1}, {NS: 1, Bytes: 1}, {NS: 1, Bytes: 1}}}
	rows, err := compare(before, after)
	require.NoError(t, err)
	require.True(t, rows[0].AllocationRegression)
}
