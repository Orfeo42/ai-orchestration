package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServer struct {
	ts *httptest.Server
}

func newTestServer(t *testing.T) testServer {
	t.Helper()
	return newProcessTestServer(t).testServer
}

func newTestServerWith(t *testing.T, proc *processHandler) testServer {
	t.Helper()
	srv, err := newServer("127.0.0.1:0", proc)
	require.NoError(t, err)
	ts := httptest.NewServer(srv.srv.Handler)
	t.Cleanup(ts.Close)
	return testServer{ts: ts}
}

func TestHealthz(t *testing.T) {
	t.Run("returns ok", func(t *testing.T) {
		s := newTestServer(t)
		resp, err := http.Get(s.ts.URL + "/healthz")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestStaticFallback(t *testing.T) {
	t.Run("unknown route serves index.html", func(t *testing.T) {
		s := newTestServer(t)
		resp, err := http.Get(s.ts.URL + "/some/spa/route")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
