package provider

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRefusedDialIsRetriedThenReported(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	listener.Close() // nothing listens: every dial is refused
	transport := &apiTransport{client: http.DefaultClient, baseURL: endpoint, headers: http.Header{}}
	started := time.Now()
	_, err = transport.postBytes(context.Background(), "/v1", []byte("{}"), false)
	require.ErrorContains(t, err, "connection refused")
	require.GreaterOrEqual(t, time.Since(started), 3*time.Second, "two retries wait 1s then 2s")
}

func TestRetryDelayKeepsRetryAfterExact(t *testing.T) {
	require.Equal(t, 2*time.Second, retryDelay(http.Header{"Retry-After": {"2"}}, 0))
	base := retryDelay(nil, 1)
	require.True(t, base >= 2*time.Second && base < 2*time.Second+250*time.Millisecond)
}
