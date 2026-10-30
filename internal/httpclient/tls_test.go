package httpclient

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInsecureTLSIsOptInAndConfinedToOneOrigin(t *testing.T) {
	var otherRequests atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherRequests.Add(1); w.WriteHeader(200) }))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	base := http.DefaultTransport.(*http.Transport)
	secure := &http.Client{Transport: Transport(context.Background(), base)}
	_, err := secure.Get(server.URL)
	require.Error(t, err)
	ctx := WithInsecureTLS(context.Background(), server.URL+"/v1", true)
	client := &http.Client{Transport: Transport(ctx, base)}
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	response.Body.Close()
	if base.TLSClientConfig != nil {
		require.False(t, base.TLSClientConfig.InsecureSkipVerify, "default transport must retain certificate verification")
	}
	_, err = secure.Get(server.URL)
	require.Error(t, err, "another connection still verifies certificates")
	_, err = client.Get(server.URL + "/redirect")
	require.Error(t, err)
	require.Zero(t, otherRequests.Load())
	disabled := &http.Client{Transport: Transport(WithInsecureTLS(ctx, server.URL, false), base)}
	_, err = disabled.Get(server.URL)
	require.Error(t, err, "false must clear an inherited scope")
}
