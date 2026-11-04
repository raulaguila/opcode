package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGradeCountsBugsFoundAndFindingsOnNoBug(t *testing.T) {
	bugs := []bug{{File: "a.go", line: 10}, {File: "a.go", line: 50}}
	result := grade(bugs, []finding{{Path: "a.go", Line: 11}, {Path: "a.go", Line: 12}, {Path: "a.go", Line: 13}, {Path: "b.go", Line: 10}})
	require.Len(t, result.found, 1)
	require.Len(t, result.missed, 1)
	require.Equal(t, 2, result.right)
	require.Len(t, result.wrong, 2, "beyond reach, or in another file, is not the bug")
}

func TestPlantedBugsAreInTheFixture(t *testing.T) {
	bugs := []bug{{File: "cart/cart.go", Code: "return items[len(items)]"}}
	require.NoError(t, locate("testdata/shop", bugs))
	require.Positive(t, bugs[0].line)
	require.Error(t, locate("testdata/shop", []bug{{File: "cart/cart.go", Code: "not there"}}))
}
