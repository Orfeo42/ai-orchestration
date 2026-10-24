package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi"
)

func writeString(t *testing.T, w http.ResponseWriter, s string) {
	t.Helper()
	_, err := io.WriteString(w, s)
	assert.NoError(t, err)
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	assert.NoError(t, json.NewEncoder(w).Encode(v))
}

func writeAPIError(t *testing.T, w http.ResponseWriter, status int, code pluginapi.ErrorCode, msg string) {
	t.Helper()
	writeJSON(t, w, status, pluginapi.APIError{Code: code, Message: msg})
}

func newPluginServer(t *testing.T, h http.HandlerFunc) (*Plugin, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewPlugin(srv.URL, "tok", srv.Client()), srv
}

func newCoreServer(t *testing.T, h http.HandlerFunc) (*Core, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewCore(srv.URL, "forgejo", "tok", srv.Client()), srv
}

func TestPluginManifest(t *testing.T) {
	t.Run("sends GET with bearer and decodes manifest", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v1/manifest", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			writeJSON(t, w, http.StatusOK, pluginapi.Manifest{Protocol: "1", Name: "ai"})
		})
		m, err := c.Manifest(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "ai", m.Name)
		assert.Equal(t, "1", m.Protocol)
	})

	t.Run("401 maps to ErrUnauthorized", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusUnauthorized, pluginapi.CodeUnauthorized, "bad token")
		})
		_, err := c.Manifest(context.Background())
		require.ErrorIs(t, err, pluginapi.ErrUnauthorized)
	})

	t.Run("invalid JSON body returns decode error", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeString(t, w, "not json")
		})
		_, err := c.Manifest(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode manifest")
	})

	t.Run("nil http client falls back to default client", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, pluginapi.Manifest{Name: "ai"})
		}))
		t.Cleanup(srv.Close)
		m, err := NewPlugin(srv.URL, "tok", nil).Manifest(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "ai", m.Name)
	})

	t.Run("transport error maps to ErrUpstream", func(t *testing.T) {
		c, srv := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {})
		srv.Close()
		_, err := c.Manifest(context.Background())
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})

	t.Run("canceled context stays detectable", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.Manifest(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("response over the limit returns ErrPayloadTooLarge", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			chunk := strings.Repeat("a", 1<<20)
			for i := int64(0); i <= pluginapi.DefaultMaxBodyBytes>>20; i++ {
				_, err := io.WriteString(w, chunk)
				if err != nil {
					return
				}
			}
		})
		_, err := c.Manifest(context.Background())
		require.ErrorIs(t, err, pluginapi.ErrPayloadTooLarge)
	})
}

func TestPluginHealth(t *testing.T) {
	t.Run("sends GET and decodes health", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v1/health", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			writeJSON(t, w, http.StatusOK, pluginapi.Health{
				Status: pluginapi.HealthDegraded,
				Checks: map[string]pluginapi.Check{"db": {Status: pluginapi.HealthDown, Message: "gone"}},
			})
		})
		h, err := c.Health(context.Background())
		require.NoError(t, err)
		assert.Equal(t, pluginapi.HealthDegraded, h.Status)
		assert.Equal(t, pluginapi.HealthDown, h.Checks["db"].Status)
	})

	t.Run("500 with non-JSON body returns HTTPError with internal code and ErrUpstream", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			writeString(t, w, "  boom \n")
		})
		_, err := c.Health(context.Background())
		var he *HTTPError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, http.StatusInternalServerError, he.Status)
		assert.Equal(t, pluginapi.CodeInternal, he.API.Code)
		assert.Equal(t, "boom", he.API.Message)
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})

	t.Run("empty error body uses status text", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		})
		_, err := c.Health(context.Background())
		var he *HTTPError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, pluginapi.CodeInternal, he.API.Code)
		assert.Equal(t, "Bad Gateway", he.API.Message)
	})
}

func TestPluginStartJob(t *testing.T) {
	req := pluginapi.JobRequest{
		JobID:     "job-1",
		ProcessID: "proc-1",
		Input:     json.RawMessage(`{"a":1}`),
		Callback:  pluginapi.Callback{ResultURL: "http://core/r", ProgressURL: "http://core/p"},
	}

	t.Run("200 sync decodes state and sends request body", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v1/actions/implement/jobs", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			var got pluginapi.JobRequest
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
			assert.Equal(t, "job-1", got.JobID)
			assert.Equal(t, "proc-1", got.ProcessID)
			assert.JSONEq(t, `{"a":1}`, string(got.Input))
			assert.Equal(t, "http://core/r", got.Callback.ResultURL)
			writeJSON(t, w, http.StatusOK, pluginapi.JobState{JobID: "job-1", Action: "implement", Status: pluginapi.JobSucceeded})
		})
		st, err := c.StartJob(context.Background(), "implement", req)
		require.NoError(t, err)
		assert.Equal(t, pluginapi.JobSucceeded, st.Status)
		assert.Equal(t, "job-1", st.JobID)
	})

	t.Run("202 async decodes running state", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusAccepted, pluginapi.JobState{JobID: "job-1", Status: pluginapi.JobRunning})
		})
		st, err := c.StartJob(context.Background(), "implement", req)
		require.NoError(t, err)
		assert.Equal(t, pluginapi.JobRunning, st.Status)
	})

	t.Run("action name is path escaped", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/actions/a%2Fb%20c/jobs", r.URL.EscapedPath())
			assert.Equal(t, "a/b c", strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/actions/"), "/jobs")[0])
			writeJSON(t, w, http.StatusOK, pluginapi.JobState{})
		})
		_, err := c.StartJob(context.Background(), "a/b c", req)
		require.NoError(t, err)
	})

	t.Run("409 job_conflict maps to ErrJobConflict", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusConflict, pluginapi.CodeJobConflict, "different input")
		})
		_, err := c.StartJob(context.Background(), "implement", req)
		require.ErrorIs(t, err, pluginapi.ErrJobConflict)
		var he *HTTPError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, http.StatusConflict, he.Status)
		assert.Equal(t, "different input", he.API.Message)
	})

	t.Run("422 maps to ErrInvalidInput", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, "bad input")
		})
		_, err := c.StartJob(context.Background(), "implement", req)
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("404 unknown_action maps to ErrUnknownAction", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusNotFound, pluginapi.CodeUnknownAction, "nope")
		})
		_, err := c.StartJob(context.Background(), "x", req)
		require.ErrorIs(t, err, pluginapi.ErrUnknownAction)
	})

	t.Run("500 with non-JSON body maps to internal code and ErrUpstream", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			writeString(t, w, "<html>oops</html>")
		})
		_, err := c.StartJob(context.Background(), "implement", req)
		var he *HTTPError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, pluginapi.CodeInternal, he.API.Code)
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})

	t.Run("unexpected 2xx status is an HTTPError", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		_, err := c.StartJob(context.Background(), "implement", req)
		var he *HTTPError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, http.StatusNoContent, he.Status)
	})
}

func TestPluginGetJob(t *testing.T) {
	t.Run("200 decodes state", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v1/jobs/job-1", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			writeJSON(t, w, http.StatusOK, pluginapi.JobState{JobID: "job-1", Status: pluginapi.JobRunning, Phase: "coding"})
		})
		st, err := c.GetJob(context.Background(), "job-1")
		require.NoError(t, err)
		assert.Equal(t, "coding", st.Phase)
	})

	t.Run("job id is path escaped", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/jobs/a%2Fb%20c", r.URL.EscapedPath())
			writeJSON(t, w, http.StatusOK, pluginapi.JobState{})
		})
		_, err := c.GetJob(context.Background(), "a/b c")
		require.NoError(t, err)
	})

	t.Run("404 job_not_found maps to ErrJobNotFound", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusNotFound, pluginapi.CodeJobNotFound, "missing")
		})
		_, err := c.GetJob(context.Background(), "job-1")
		require.ErrorIs(t, err, pluginapi.ErrJobNotFound)
	})
}

func TestPluginCancelJob(t *testing.T) {
	t.Run("200 terminal job decodes state", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v1/jobs/job-1/cancel", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			writeJSON(t, w, http.StatusOK, pluginapi.JobState{JobID: "job-1", Status: pluginapi.JobSucceeded})
		})
		st, err := c.CancelJob(context.Background(), "job-1")
		require.NoError(t, err)
		assert.Equal(t, pluginapi.JobSucceeded, st.Status)
	})

	t.Run("202 in progress decodes state", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusAccepted, pluginapi.JobState{JobID: "job-1", Status: pluginapi.JobRunning})
		})
		st, err := c.CancelJob(context.Background(), "job-1")
		require.NoError(t, err)
		assert.Equal(t, pluginapi.JobRunning, st.Status)
	})

	t.Run("404 maps to ErrJobNotFound", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusNotFound, pluginapi.CodeJobNotFound, "missing")
		})
		_, err := c.CancelJob(context.Background(), "job-1")
		require.ErrorIs(t, err, pluginapi.ErrJobNotFound)
	})
}

func sseHandler(t *testing.T, payload string, check func(r *http.Request)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeString(t, w, payload)
	}
}

func TestPluginStreamEvents(t *testing.T) {
	t.Run("dispatches two events in order and sends after query and headers", func(t *testing.T) {
		payload := "data: {\"seq\":1,\"kind\":\"phase\"}\n\ndata: {\"seq\":2,\"kind\":\"result\"}\n\n"
		c, _ := newPluginServer(t, sseHandler(t, payload, func(r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v1/jobs/job-1/events", r.URL.Path)
			assert.Equal(t, "7", r.URL.Query().Get("after"))
			assert.Equal(t, "text/event-stream", r.Header.Get("Accept"))
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		}))
		var got []pluginapi.Event
		err := c.StreamEvents(context.Background(), "job-1", 7, func(e pluginapi.Event) error {
			got = append(got, e)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, int64(1), got[0].Seq)
		assert.Equal(t, "phase", got[0].Kind)
		assert.Equal(t, int64(2), got[1].Seq)
		assert.Equal(t, "result", got[1].Kind)
	})

	t.Run("multi-line data is joined with newline", func(t *testing.T) {
		payload := "data: {\"seq\":3,\ndata: \"kind\":\"phase\"}\n\n"
		c, _ := newPluginServer(t, sseHandler(t, payload, nil))
		var got []pluginapi.Event
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error {
			got = append(got, e)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(3), got[0].Seq)
		assert.Equal(t, "phase", got[0].Kind)
	})

	t.Run("comments and event id retry lines are ignored", func(t *testing.T) {
		payload := ": keepalive\nevent: message\nid: 9\nretry: 100\ndata:{\"seq\":4,\"kind\":\"x\"}\n\n: another\n\n"
		c, _ := newPluginServer(t, sseHandler(t, payload, nil))
		var got []pluginapi.Event
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error {
			got = append(got, e)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(4), got[0].Seq)
	})

	t.Run("CRLF line endings are handled", func(t *testing.T) {
		payload := "data: {\"seq\":5,\"kind\":\"x\"}\r\n\r\n"
		c, _ := newPluginServer(t, sseHandler(t, payload, nil))
		var got []pluginapi.Event
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error {
			got = append(got, e)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(5), got[0].Seq)
	})

	t.Run("EOF with pending event dispatches it", func(t *testing.T) {
		c, _ := newPluginServer(t, sseHandler(t, "data: {\"seq\":6,\"kind\":\"x\"}", nil))
		var got []pluginapi.Event
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error {
			got = append(got, e)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(6), got[0].Seq)
	})

	t.Run("event larger than 64 KiB is delivered", func(t *testing.T) {
		big := strings.Repeat("x", 200<<10)
		payload := "data: {\"seq\":1,\"kind\":\"big\",\"data\":\"" + big + "\"}\n\n"
		c, _ := newPluginServer(t, sseHandler(t, payload, nil))
		var got []pluginapi.Event
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error {
			got = append(got, e)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Len(t, got[0].Data, len(big)+2)
	})

	t.Run("fn error stops the stream and is returned", func(t *testing.T) {
		payload := "data: {\"seq\":1,\"kind\":\"a\"}\n\ndata: {\"seq\":2,\"kind\":\"b\"}\n\n"
		c, _ := newPluginServer(t, sseHandler(t, payload, nil))
		stop := errors.New("stop")
		calls := 0
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error {
			calls++
			return stop
		})
		require.ErrorIs(t, err, stop)
		assert.Equal(t, 1, calls)
	})

	t.Run("invalid event JSON returns decode error", func(t *testing.T) {
		c, _ := newPluginServer(t, sseHandler(t, "data: nope\n\n", nil))
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error { return nil })
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode event")
	})

	t.Run("event over the limit returns ErrPayloadTooLarge", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			chunk := strings.Repeat("a", 1<<20)
			writeString(t, w, "data: ")
			for i := int64(0); i <= pluginapi.DefaultMaxBodyBytes>>20; i++ {
				if _, err := io.WriteString(w, chunk); err != nil {
					return
				}
			}
		})
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error { return nil })
		require.ErrorIs(t, err, pluginapi.ErrPayloadTooLarge)
	})

	t.Run("404 returns HTTPError with ErrJobNotFound", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusNotFound, pluginapi.CodeJobNotFound, "missing")
		})
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error { return nil })
		var he *HTTPError
		require.ErrorAs(t, err, &he)
		require.ErrorIs(t, err, pluginapi.ErrJobNotFound)
	})

	t.Run("transport error maps to ErrUpstream", func(t *testing.T) {
		c, srv := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {})
		srv.Close()
		err := c.StreamEvents(context.Background(), "job-1", 0, func(e pluginapi.Event) error { return nil })
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})

	t.Run("context cancel mid-stream returns the context error", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			writeString(t, w, "data: {\"seq\":1,\"kind\":\"a\"}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := c.StreamEvents(ctx, "job-1", 0, func(e pluginapi.Event) error {
			cancel()
			return nil
		})
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestPluginSendMessage(t *testing.T) {
	t.Run("202 sends message body", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v1/jobs/job-1/messages", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			var m pluginapi.Message
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&m))
			assert.Equal(t, "hello", m.Text)
			w.WriteHeader(http.StatusAccepted)
		})
		require.NoError(t, c.SendMessage(context.Background(), "job-1", pluginapi.Message{Text: "hello"}))
	})

	t.Run("job id is path escaped", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/jobs/a%2Fb/messages", r.URL.EscapedPath())
			w.WriteHeader(http.StatusAccepted)
		})
		require.NoError(t, c.SendMessage(context.Background(), "a/b", pluginapi.Message{Text: "x"}))
	})

	t.Run("409 unsupported maps to ErrUnsupported", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusConflict, pluginapi.CodeUnsupported, "no inject")
		})
		err := c.SendMessage(context.Background(), "job-1", pluginapi.Message{Text: "x"})
		require.ErrorIs(t, err, pluginapi.ErrUnsupported)
	})

	t.Run("409 job_not_active maps to ErrJobNotActive", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusConflict, pluginapi.CodeJobNotActive, "done")
		})
		err := c.SendMessage(context.Background(), "job-1", pluginapi.Message{Text: "x"})
		require.ErrorIs(t, err, pluginapi.ErrJobNotActive)
	})

	t.Run("200 is not accepted", func(t *testing.T) {
		c, _ := newPluginServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		err := c.SendMessage(context.Background(), "job-1", pluginapi.Message{Text: "x"})
		var he *HTTPError
		require.ErrorAs(t, err, &he)
	})
}

func TestCoreEmitTrigger(t *testing.T) {
	ev := pluginapi.TriggerEvent{
		Plugin:  "forgejo",
		Trigger: "issue.labeled",
		Subject: pluginapi.Subject{Kind: "issue", Key: "o/r#1"},
		Payload: json.RawMessage(`{"n":1}`),
	}

	t.Run("202 sends bearer and plugin name and decodes process ids", func(t *testing.T) {
		c, _ := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/triggers", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			assert.Equal(t, "forgejo", r.Header.Get("X-Plugin-Name"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			var got pluginapi.TriggerEvent
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
			assert.Equal(t, "issue.labeled", got.Trigger)
			assert.Equal(t, "o/r#1", got.Subject.Key)
			writeJSON(t, w, http.StatusAccepted, pluginapi.TriggerAccepted{ProcessIDs: []string{"p1", "p2"}})
		})
		acc, err := c.EmitTrigger(context.Background(), ev)
		require.NoError(t, err)
		assert.Equal(t, []string{"p1", "p2"}, acc.ProcessIDs)
	})

	t.Run("422 maps to ErrInvalidInput", func(t *testing.T) {
		c, _ := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, "payload")
		})
		_, err := c.EmitTrigger(context.Background(), ev)
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("401 maps to ErrUnauthorized", func(t *testing.T) {
		c, _ := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(t, w, http.StatusUnauthorized, pluginapi.CodeUnauthorized, "token")
		})
		_, err := c.EmitTrigger(context.Background(), ev)
		require.ErrorIs(t, err, pluginapi.ErrUnauthorized)
	})

	t.Run("transport error maps to ErrUpstream", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {})
		srv.Close()
		_, err := c.EmitTrigger(context.Background(), ev)
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})

	t.Run("nil http client falls back to default client", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusAccepted, pluginapi.TriggerAccepted{})
		}))
		t.Cleanup(srv.Close)
		_, err := NewCore(srv.URL, "forgejo", "tok", nil).EmitTrigger(context.Background(), ev)
		require.NoError(t, err)
	})
}

func TestCoreComplete(t *testing.T) {
	result := pluginapi.Result{JobID: "job-1", Outcome: pluginapi.OutcomeOK}

	t.Run("204 posts result to result url without authorization", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/cb/result", r.URL.EscapedPath())
			assert.Equal(t, "sig=abc", r.URL.RawQuery)
			assert.Empty(t, r.Header.Get("Authorization"))
			assert.Empty(t, r.Header.Get("X-Plugin-Name"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			var got pluginapi.Result
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
			assert.Equal(t, "job-1", got.JobID)
			assert.Equal(t, pluginapi.OutcomeOK, got.Outcome)
			w.WriteHeader(http.StatusNoContent)
		})
		cb := pluginapi.Callback{ResultURL: srv.URL + "/cb/result?sig=abc"}
		require.NoError(t, c.Complete(context.Background(), cb, result))
	})

	t.Run("410 with empty body maps to ErrJobNotActive", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusGone)
		})
		err := c.Complete(context.Background(), pluginapi.Callback{ResultURL: srv.URL + "/cb"}, result)
		var he *HTTPError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, http.StatusGone, he.Status)
		require.ErrorIs(t, err, pluginapi.ErrJobNotActive)
	})

	t.Run("any 2xx is accepted", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, map[string]string{"ok": "yes"})
		})
		require.NoError(t, c.Complete(context.Background(), pluginapi.Callback{ResultURL: srv.URL + "/cb"}, result))
	})

	t.Run("503 maps to ErrUpstream", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		err := c.Complete(context.Background(), pluginapi.Callback{ResultURL: srv.URL + "/cb"}, result)
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})

	t.Run("transport error maps to ErrUpstream", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {})
		url := srv.URL + "/cb"
		srv.Close()
		err := c.Complete(context.Background(), pluginapi.Callback{ResultURL: url}, result)
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})

	t.Run("invalid url returns build error", func(t *testing.T) {
		c := NewCore("http://core", "forgejo", "tok", nil)
		err := c.Complete(context.Background(), pluginapi.Callback{ResultURL: "http://bad host/\x7f"}, result)
		require.Error(t, err)
	})
}

func TestCoreProgress(t *testing.T) {
	progress := pluginapi.Progress{JobID: "job-1", Phase: "coding", At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}

	t.Run("204 posts progress to progress url without authorization", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/cb/progress", r.URL.Path)
			assert.Empty(t, r.Header.Get("Authorization"))
			var got pluginapi.Progress
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
			assert.Equal(t, "coding", got.Phase)
			assert.Equal(t, "job-1", got.JobID)
			w.WriteHeader(http.StatusNoContent)
		})
		cb := pluginapi.Callback{ProgressURL: srv.URL + "/cb/progress"}
		require.NoError(t, c.Progress(context.Background(), cb, progress))
	})

	t.Run("410 maps to ErrJobNotActive", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusGone)
		})
		err := c.Progress(context.Background(), pluginapi.Callback{ProgressURL: srv.URL + "/cb"}, progress)
		require.ErrorIs(t, err, pluginapi.ErrJobNotActive)
	})

	t.Run("transport error maps to ErrUpstream", func(t *testing.T) {
		c, srv := newCoreServer(t, func(w http.ResponseWriter, r *http.Request) {})
		url := srv.URL + "/cb"
		srv.Close()
		err := c.Progress(context.Background(), pluginapi.Callback{ProgressURL: url}, progress)
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})
}

func TestHTTPErrorError(t *testing.T) {
	t.Run("formats status code and message", func(t *testing.T) {
		e := &HTTPError{Status: 409, API: pluginapi.APIError{Code: pluginapi.CodeJobConflict, Message: "different input"}}
		assert.Equal(t, "plugin api: status 409: job_conflict: different input", e.Error())
	})
}

func TestHTTPErrorUnwrap(t *testing.T) {
	unwrap := func(status int, code pluginapi.ErrorCode) error {
		return (&HTTPError{Status: status, API: pluginapi.APIError{Code: code}}).Unwrap()
	}

	t.Run("invalid_input", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrInvalidInput, unwrap(422, pluginapi.CodeInvalidInput))
	})
	t.Run("unknown_action", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrUnknownAction, unwrap(404, pluginapi.CodeUnknownAction))
	})
	t.Run("job_not_found", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrJobNotFound, unwrap(404, pluginapi.CodeJobNotFound))
	})
	t.Run("job_conflict", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrJobConflict, unwrap(409, pluginapi.CodeJobConflict))
	})
	t.Run("job_not_active", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrJobNotActive, unwrap(409, pluginapi.CodeJobNotActive))
	})
	t.Run("unsupported", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrUnsupported, unwrap(409, pluginapi.CodeUnsupported))
	})
	t.Run("unauthorized", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrUnauthorized, unwrap(401, pluginapi.CodeUnauthorized))
	})
	t.Run("payload_too_large", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrPayloadTooLarge, unwrap(413, pluginapi.CodePayloadTooLarge))
	})
	t.Run("upstream_failure", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrUpstream, unwrap(502, pluginapi.CodeUpstreamFailure))
	})
	t.Run("protocol_mismatch", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrUnsupportedProtocol, unwrap(400, pluginapi.CodeProtocolMismatch))
	})
	t.Run("code wins over status", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrJobConflict, unwrap(500, pluginapi.CodeJobConflict))
	})
	t.Run("status 410 without known code is ErrJobNotActive", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrJobNotActive, unwrap(410, pluginapi.CodeInternal))
	})
	t.Run("status 401 without known code is ErrUnauthorized", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrUnauthorized, unwrap(401, pluginapi.CodeInternal))
	})
	t.Run("status 5xx without known code is ErrUpstream", func(t *testing.T) {
		assert.Equal(t, pluginapi.ErrUpstream, unwrap(503, pluginapi.CodeInternal))
	})
	t.Run("other status without known code is nil", func(t *testing.T) {
		assert.NoError(t, unwrap(400, pluginapi.CodeInternal))
	})
}
