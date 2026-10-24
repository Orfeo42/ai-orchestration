package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/pluginapi/conformance"
	"github.com/Orfeo42/ai-orchestration/pluginapi/server"
)

func TestConformance(t *testing.T) {
	t.Run("echo plugin passes the protocol suite", func(t *testing.T) {
		acts, err := actions()
		require.NoError(t, err)
		inf, err := info()
		require.NoError(t, err)
		srv, err := server.New(server.Config{Token: "tok"}, inf, acts...)
		require.NoError(t, err)
		ts := httptest.NewServer(srv)
		t.Cleanup(ts.Close)
		conformance.Run(t, conformance.Target{
			BaseURL:    ts.URL,
			Token:      "tok",
			HTTPClient: http.DefaultClient,
			Fixtures: map[string]json.RawMessage{
				"echo": json.RawMessage(`{"message":"hi"}`),
				"wait": json.RawMessage(`{"message":"hi","seconds":1}`),
			},
		})
	})
}
