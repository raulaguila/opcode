package chat

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReplyDurationUsesStoredUnixSeconds(t *testing.T) {
	for _, tc := range []struct {
		start, end int64
		want       string
	}{
		{1700000000, 1700000000, "<1s"},
		{1700000000, 1700000001, "1s"},
		{1700000000, 1700000033, "33s"},
		{1700000000, 1700000061, "1m01s"},
		{1700000000, 1700000120, "2m00s"},
		{0, 1700000000, "—"},
		{1700000001, 1700000000, "—"},
	} {
		require.Equal(t, tc.want, formatTimestampDiff(tc.start, tc.end))
	}
}
