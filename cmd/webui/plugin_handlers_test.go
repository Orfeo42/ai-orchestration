package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/plugins"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type pluginTestServer struct {
	url       string
	registry  *MockPluginRegistry
	catalog   *MockPluginCatalog
	callbacks *MockJobCallbacks
	verifier  *MockCallbackVerifier
	auth      *MockPluginAuth
	starter   *MockEventStarter
	store     *MockWorkflowStore
	codec     *MockWorkflowCodec
}

func newPluginTestServer(t *testing.T) pluginTestServer {
	t.Helper()
	s := pluginTestServer{
		registry:  &MockPluginRegistry{},
		catalog:   &MockPluginCatalog{},
		callbacks: &MockJobCallbacks{},
		verifier:  &MockCallbackVerifier{},
		auth:      &MockPluginAuth{},
		starter:   &MockEventStarter{},
		store:     &MockWorkflowStore{},
		codec:     &MockWorkflowCodec{},
	}
	h := &pluginHandler{
		registry:  s.registry,
		catalog:   s.catalog,
		callbacks: s.callbacks,
		verifier:  s.verifier,
		auth:      s.auth,
		starter:   s.starter,
		store:     s.store,
		codec:     s.codec,
	}
	srv, err := newServer("127.0.0.1:0", &processHandler{}, h)
	require.NoError(t, err)
	ts := httptest.NewServer(srv.srv.Handler)
	t.Cleanup(ts.Close)
	s.url = ts.URL
	return s
}

func sendWith(t *testing.T, method, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(body))
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	return resp
}

func decodeAPIError(t *testing.T, resp *http.Response) pluginapi.APIError {
	t.Helper()
	return decode[pluginapi.APIError](t, resp)
}

func TestGetCatalog(t *testing.T) {
	t.Run("empty catalog returns empty arrays", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.catalog.DefsFunc = func() []step.Def { return nil }
		s.catalog.TriggersFunc = func() []plugins.TriggerDef { return nil }
		resp := send(t, http.MethodGet, s.url+"/api/catalog", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[map[string]json.RawMessage](t, resp)
		assert.JSONEq(t, `[]`, string(body["steps"]))
		assert.JSONEq(t, `[]`, string(body["triggers"]))
	})

	t.Run("maps step defs with non-null outcomes", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.catalog.DefsFunc = func() []step.Def {
			return []step.Def{
				{Name: "claude.assess", Description: "assess", Queue: "claude", Outcomes: []step.Outcome{"ok", "fail"}},
				{Name: "forgejo/comment", Queue: "default", InputSchema: json.RawMessage(`{"type":"object"}`), Plugin: "forgejo"},
			}
		}
		s.catalog.TriggersFunc = func() []plugins.TriggerDef {
			return []plugins.TriggerDef{{Plugin: "forgejo", Name: "issue", PayloadSchema: json.RawMessage(`{}`)}}
		}
		resp := send(t, http.MethodGet, s.url+"/api/catalog", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[catalogResponse](t, resp)
		require.Len(t, body.Steps, 2)
		assert.Equal(t, "claude.assess", body.Steps[0].Name)
		assert.Equal(t, []step.Outcome{"ok", "fail"}, body.Steps[0].Outcomes)
		assert.Empty(t, body.Steps[0].Plugin)
		assert.NotNil(t, body.Steps[1].Outcomes)
		assert.Equal(t, "forgejo", body.Steps[1].Plugin)
		assert.JSONEq(t, `{"type":"object"}`, string(body.Steps[1].InputSchema))
		require.Len(t, body.Triggers, 1)
		assert.Equal(t, "issue", body.Triggers[0].Name)
	})
}

func TestListPlugins(t *testing.T) {
	t.Run("nil list returns empty array", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.ListFunc = func(context.Context) ([]plugins.Record, error) { return nil, nil }
		resp := send(t, http.MethodGet, s.url+"/api/plugins", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.JSONEq(t, `[]`, string(decode[json.RawMessage](t, resp)))
	})

	t.Run("registry error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.ListFunc = func(context.Context) ([]plugins.Record, error) { return nil, assert.AnError }
		resp := send(t, http.MethodGet, s.url+"/api/plugins", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestRegisterPlugin(t *testing.T) {
	t.Run("ok returns 201 with record", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RegisterFunc = func(_ context.Context, name, baseURL string) (plugins.Record, error) {
			assert.Equal(t, "forgejo", name)
			assert.Equal(t, "http://forgejo:9000", baseURL)
			return plugins.Record{Name: name, BaseURL: baseURL, Status: plugins.StatusOK}, nil
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins", `{"name":"forgejo","base_url":"http://forgejo:9000"}`)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Equal(t, "forgejo", decode[plugins.Record](t, resp).Name)
	})

	t.Run("missing field returns 400", func(t *testing.T) {
		s := newPluginTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/plugins", `{"name":"forgejo"}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Empty(t, s.registry.RegisterCalls())
	})

	t.Run("invalid json returns 400", func(t *testing.T) {
		s := newPluginTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/plugins", `{`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("no token returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RegisterFunc = func(context.Context, string, string) (plugins.Record, error) {
			return plugins.Record{}, plugins.ErrNoToken
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins", `{"name":"a","base_url":"http://a"}`)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("name mismatch returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RegisterFunc = func(context.Context, string, string) (plugins.Record, error) {
			return plugins.Record{}, plugins.ErrNameMismatch
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins", `{"name":"a","base_url":"http://a"}`)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("invalid manifest returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RegisterFunc = func(context.Context, string, string) (plugins.Record, error) {
			return plugins.Record{}, pluginapi.ErrInvalidManifest
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins", `{"name":"a","base_url":"http://a"}`)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("unreachable returns 502", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RegisterFunc = func(context.Context, string, string) (plugins.Record, error) {
			return plugins.Record{}, plugins.ErrUnreachable
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins", `{"name":"a","base_url":"http://a"}`)
		assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	})

	t.Run("other error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RegisterFunc = func(context.Context, string, string) (plugins.Record, error) {
			return plugins.Record{}, assert.AnError
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins", `{"name":"a","base_url":"http://a"}`)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestRefreshPlugin(t *testing.T) {
	t.Run("ok returns 200", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RefreshFunc = func(_ context.Context, name string) (plugins.Record, error) {
			return plugins.Record{Name: name}, nil
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins/forgejo/refresh", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "forgejo", decode[plugins.Record](t, resp).Name)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RefreshFunc = func(context.Context, string) (plugins.Record, error) {
			return plugins.Record{}, plugins.ErrNotFound
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins/x/refresh", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("other error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RefreshFunc = func(context.Context, string) (plugins.Record, error) {
			return plugins.Record{}, assert.AnError
		}
		resp := send(t, http.MethodPost, s.url+"/api/plugins/x/refresh", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestRemovePlugin(t *testing.T) {
	t.Run("ok returns 204", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RemoveFunc = func(context.Context, string) error { return nil }
		resp := send(t, http.MethodDelete, s.url+"/api/plugins/forgejo", "")
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		require.Len(t, s.registry.RemoveCalls(), 1)
		assert.Equal(t, "forgejo", s.registry.RemoveCalls()[0].Name)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RemoveFunc = func(context.Context, string) error { return plugins.ErrNotFound }
		resp := send(t, http.MethodDelete, s.url+"/api/plugins/x", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("in use returns 409", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RemoveFunc = func(context.Context, string) error { return plugins.ErrInUse }
		resp := send(t, http.MethodDelete, s.url+"/api/plugins/x", "")
		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})

	t.Run("other error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.registry.RemoveFunc = func(context.Context, string) error { return assert.AnError }
		resp := send(t, http.MethodDelete, s.url+"/api/plugins/x", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

type joinedErrors struct{ errs []error }

func (j joinedErrors) Error() string   { return "joined" }
func (j joinedErrors) Unwrap() []error { return j.errs }

func TestSaveWorkflow(t *testing.T) {
	def := workflow.Definition{Name: "impl", Start: "a"}

	t.Run("parse error returns 400", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.codec.ParseFunc = func([]byte) (workflow.Definition, error) { return workflow.Definition{}, assert.AnError }
		resp := send(t, http.MethodPost, s.url+"/api/workflows", `{}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Empty(t, s.codec.ValidateCalls())
	})

	t.Run("joined validation errors return 422 with problems", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.codec.ParseFunc = func([]byte) (workflow.Definition, error) { return def, nil }
		s.codec.ValidateFunc = func(workflow.Definition) error {
			return joinedErrors{errs: []error{assert.AnError, context.Canceled}}
		}
		resp := send(t, http.MethodPost, s.url+"/api/workflows", `{}`)
		require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
		body := decode[workflowProblems](t, resp)
		assert.Equal(t, []string{assert.AnError.Error(), context.Canceled.Error()}, body.Problems)
		assert.Empty(t, s.store.InTxCalls())
	})

	t.Run("single validation error returns one problem", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.codec.ParseFunc = func([]byte) (workflow.Definition, error) { return def, nil }
		s.codec.ValidateFunc = func(workflow.Definition) error { return assert.AnError }
		resp := send(t, http.MethodPost, s.url+"/api/workflows", `{}`)
		require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
		assert.Equal(t, []string{assert.AnError.Error()}, decode[workflowProblems](t, resp).Problems)
	})

	t.Run("yaml error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.codec.ParseFunc = func([]byte) (workflow.Definition, error) { return def, nil }
		s.codec.ValidateFunc = func(workflow.Definition) error { return nil }
		s.codec.YAMLFunc = func(workflow.Definition) ([]byte, error) { return nil, assert.AnError }
		resp := send(t, http.MethodPost, s.url+"/api/workflows", `{}`)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})

	t.Run("valid definition upserts and sets current in one tx", func(t *testing.T) {
		s := newPluginTestServer(t)
		yaml := []byte("name: impl\n")
		s.codec.ParseFunc = func([]byte) (workflow.Definition, error) { return def, nil }
		s.codec.ValidateFunc = func(workflow.Definition) error { return nil }
		s.codec.YAMLFunc = func(workflow.Definition) ([]byte, error) { return yaml, nil }
		tx := &process.MockTx{
			UpsertWorkflowVersionFunc: func(_ context.Context, v process.WorkflowVersion) (process.WorkflowVersion, error) {
				assert.Equal(t, "impl", v.Name)
				assert.Equal(t, workflow.Hash(yaml), v.Hash)
				assert.Equal(t, string(yaml), v.SourceYAML)
				assert.JSONEq(t, `{"name":"impl","trigger":{},"start":"a","steps":null}`, string(v.Definition))
				v.ID = 7
				return v, nil
			},
			SetCurrentWorkflowFunc: func(_ context.Context, name string, id int64) error {
				assert.Equal(t, "impl", name)
				assert.Equal(t, int64(7), id)
				return nil
			},
		}
		s.store.InTxFunc = func(ctx context.Context, fn func(context.Context, process.Tx) error) error { return fn(ctx, tx) }
		resp := send(t, http.MethodPost, s.url+"/api/workflows", `{"name":"impl"}`)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, int64(7), decode[process.WorkflowVersion](t, resp).ID)
		assert.Len(t, tx.UpsertWorkflowVersionCalls(), 1)
		assert.Len(t, tx.SetCurrentWorkflowCalls(), 1)
	})

	t.Run("tx error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.codec.ParseFunc = func([]byte) (workflow.Definition, error) { return def, nil }
		s.codec.ValidateFunc = func(workflow.Definition) error { return nil }
		s.codec.YAMLFunc = func(workflow.Definition) ([]byte, error) { return []byte("x"), nil }
		s.store.InTxFunc = func(context.Context, func(context.Context, process.Tx) error) error { return assert.AnError }
		resp := send(t, http.MethodPost, s.url+"/api/workflows", `{}`)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestDeleteWorkflow(t *testing.T) {
	t.Run("deleted returns 204", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.store.DeleteWorkflowFunc = func(context.Context, string) (bool, error) { return true, nil }
		resp := send(t, http.MethodDelete, s.url+"/api/workflows/impl", "")
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		assert.Equal(t, "impl", s.store.DeleteWorkflowCalls()[0].Name)
	})

	t.Run("missing returns 404", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.store.DeleteWorkflowFunc = func(context.Context, string) (bool, error) { return false, nil }
		resp := send(t, http.MethodDelete, s.url+"/api/workflows/impl", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("store error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.store.DeleteWorkflowFunc = func(context.Context, string) (bool, error) { return false, assert.AnError }
		resp := send(t, http.MethodDelete, s.url+"/api/workflows/impl", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestJobResult(t *testing.T) {
	const path = "/api/v1/jobs/j1/result?sig=x"

	t.Run("bad signature returns 401", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error {
			return plugins.ErrInvalidSignature
		}
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.Equal(t, pluginapi.CodeUnauthorized, decodeAPIError(t, resp).Code)
		assert.Empty(t, s.callbacks.ResultCalls())
	})

	t.Run("too large body returns 413", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		big := strings.Repeat("a", int(pluginapi.DefaultMaxBodyBytes)+1)
		resp := send(t, http.MethodPost, s.url+path, big)
		require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
		assert.Equal(t, pluginapi.CodePayloadTooLarge, decodeAPIError(t, resp).Code)
	})

	t.Run("bad json returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		resp := send(t, http.MethodPost, s.url+path, `{`)
		require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
		assert.Equal(t, pluginapi.CodeInvalidInput, decodeAPIError(t, resp).Code)
	})

	t.Run("ok returns 204 and forwards result", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(kind plugins.CallbackKind, id string, q url.Values, _ time.Time) error {
			assert.Equal(t, plugins.CallbackResult, kind)
			assert.Equal(t, "j1", id)
			assert.Equal(t, "x", q.Get("sig"))
			return nil
		}
		s.callbacks.ResultFunc = func(_ context.Context, id string, res pluginapi.Result) error {
			assert.Equal(t, "j1", id)
			assert.Equal(t, pluginapi.Outcome("done"), res.Outcome)
			return nil
		}
		resp := send(t, http.MethodPost, s.url+path, `{"job_id":"j1","outcome":"done"}`)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		assert.Len(t, s.callbacks.ResultCalls(), 1)
	})

	t.Run("not active returns 410", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		s.callbacks.ResultFunc = func(context.Context, string, pluginapi.Result) error { return process.ErrNotActive }
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		require.Equal(t, http.StatusGone, resp.StatusCode)
		assert.Equal(t, pluginapi.CodeJobNotActive, decodeAPIError(t, resp).Code)
	})

	t.Run("not found returns 410", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		s.callbacks.ResultFunc = func(context.Context, string, pluginapi.Result) error { return process.ErrNotFound }
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		assert.Equal(t, http.StatusGone, resp.StatusCode)
	})

	t.Run("not plugin step returns 410", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		s.callbacks.ResultFunc = func(context.Context, string, pluginapi.Result) error { return plugins.ErrNotPluginStep }
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		assert.Equal(t, http.StatusGone, resp.StatusCode)
	})

	t.Run("invalid input returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		s.callbacks.ResultFunc = func(context.Context, string, pluginapi.Result) error { return pluginapi.ErrInvalidInput }
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("internal error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		s.callbacks.ResultFunc = func(context.Context, string, pluginapi.Result) error { return assert.AnError }
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		assert.Equal(t, pluginapi.CodeInternal, decodeAPIError(t, resp).Code)
	})
}

func TestJobProgress(t *testing.T) {
	const path = "/api/v1/jobs/j1/progress?sig=x"

	t.Run("bad signature returns 401", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error {
			return plugins.ErrExpiredSignature
		}
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("ok returns 204 and forwards progress", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(kind plugins.CallbackKind, _ string, _ url.Values, _ time.Time) error {
			assert.Equal(t, plugins.CallbackProgress, kind)
			return nil
		}
		s.callbacks.ProgressFunc = func(_ context.Context, id string, p pluginapi.Progress) error {
			assert.Equal(t, "j1", id)
			assert.Equal(t, "thinking", p.Phase)
			return nil
		}
		resp := send(t, http.MethodPost, s.url+path, `{"phase":"thinking"}`)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	t.Run("bad json returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		resp := send(t, http.MethodPost, s.url+path, `[`)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("not active returns 410", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		s.callbacks.ProgressFunc = func(context.Context, string, pluginapi.Progress) error { return process.ErrNotActive }
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		assert.Equal(t, http.StatusGone, resp.StatusCode)
	})

	t.Run("internal error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.verifier.VerifyFunc = func(plugins.CallbackKind, string, url.Values, time.Time) error { return nil }
		s.callbacks.ProgressFunc = func(context.Context, string, pluginapi.Progress) error { return assert.AnError }
		resp := send(t, http.MethodPost, s.url+path, `{}`)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestPostTrigger(t *testing.T) {
	const path = "/api/v1/triggers"
	const body = `{"plugin":"forgejo","trigger":"issue","subject":{"kind":"issue","key":"o/r#1"}}`
	authed := map[string]string{
		pluginapi.HeaderPluginName:    "forgejo",
		pluginapi.HeaderAuthorization: pluginapi.BearerPrefix + "tok",
	}

	okAuth := func(s pluginTestServer) {
		s.auth.AuthenticateFunc = func(string, string) error { return nil }
		s.catalog.ValidateTriggerFunc = func(pluginapi.TriggerEvent) error { return nil }
	}

	t.Run("missing plugin header returns 401", func(t *testing.T) {
		s := newPluginTestServer(t)
		resp := sendWith(t, http.MethodPost, s.url+path, body, map[string]string{pluginapi.HeaderAuthorization: pluginapi.BearerPrefix + "tok"})
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.Equal(t, pluginapi.CodeUnauthorized, decodeAPIError(t, resp).Code)
		assert.Empty(t, s.auth.AuthenticateCalls())
	})

	t.Run("missing bearer returns 401", func(t *testing.T) {
		s := newPluginTestServer(t)
		resp := sendWith(t, http.MethodPost, s.url+path, body, map[string]string{pluginapi.HeaderPluginName: "forgejo"})
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("bad token returns 401 before reading body", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.auth.AuthenticateFunc = func(name, token string) error {
			assert.Equal(t, "forgejo", name)
			assert.Equal(t, "tok", token)
			return plugins.ErrUnauthorized
		}
		resp := sendWith(t, http.MethodPost, s.url+path, `{`, authed)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.Empty(t, s.catalog.ValidateTriggerCalls())
	})

	t.Run("too large body returns 413", func(t *testing.T) {
		s := newPluginTestServer(t)
		okAuth(s)
		big := strings.Repeat("a", int(pluginapi.DefaultMaxBodyBytes)+1)
		resp := sendWith(t, http.MethodPost, s.url+path, big, authed)
		assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	})

	t.Run("bad json returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		okAuth(s)
		resp := sendWith(t, http.MethodPost, s.url+path, `{`, authed)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("plugin mismatch returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		okAuth(s)
		resp := sendWith(t, http.MethodPost, s.url+path, `{"plugin":"other","trigger":"issue","subject":{"key":"k"}}`, authed)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
		assert.Empty(t, s.catalog.ValidateTriggerCalls())
	})

	t.Run("invalid payload returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.auth.AuthenticateFunc = func(string, string) error { return nil }
		s.catalog.ValidateTriggerFunc = func(pluginapi.TriggerEvent) error { return plugins.ErrInvalidPayload }
		resp := sendWith(t, http.MethodPost, s.url+path, body, authed)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("unknown trigger returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.auth.AuthenticateFunc = func(string, string) error { return nil }
		s.catalog.ValidateTriggerFunc = func(pluginapi.TriggerEvent) error { return plugins.ErrUnknownTrigger }
		resp := sendWith(t, http.MethodPost, s.url+path, body, authed)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("unknown plugin returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		s.auth.AuthenticateFunc = func(string, string) error { return nil }
		s.catalog.ValidateTriggerFunc = func(pluginapi.TriggerEvent) error { return plugins.ErrNotFound }
		resp := sendWith(t, http.MethodPost, s.url+path, body, authed)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("empty subject key returns 422", func(t *testing.T) {
		s := newPluginTestServer(t)
		okAuth(s)
		resp := sendWith(t, http.MethodPost, s.url+path, `{"plugin":"forgejo","trigger":"issue","subject":{"kind":"issue"}}`, authed)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
		assert.Empty(t, s.starter.StartFromEventCalls())
	})

	t.Run("ok returns 202 with process ids", func(t *testing.T) {
		s := newPluginTestServer(t)
		okAuth(s)
		s.starter.StartFromEventFunc = func(_ context.Context, ev pluginapi.TriggerEvent) ([]string, error) {
			assert.Equal(t, "o/r#1", ev.Subject.Key)
			return []string{"p1", "p2"}, nil
		}
		resp := sendWith(t, http.MethodPost, s.url+path, body, authed)
		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		assert.Equal(t, []string{"p1", "p2"}, decode[pluginapi.TriggerAccepted](t, resp).ProcessIDs)
	})

	t.Run("empty ids encode as empty array", func(t *testing.T) {
		s := newPluginTestServer(t)
		okAuth(s)
		s.starter.StartFromEventFunc = func(context.Context, pluginapi.TriggerEvent) ([]string, error) { return nil, nil }
		resp := sendWith(t, http.MethodPost, s.url+path, body, authed)
		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		raw := decode[map[string]json.RawMessage](t, resp)
		assert.JSONEq(t, `[]`, string(raw["process_ids"]))
	})

	t.Run("starter error returns 500", func(t *testing.T) {
		s := newPluginTestServer(t)
		okAuth(s)
		s.starter.StartFromEventFunc = func(context.Context, pluginapi.TriggerEvent) ([]string, error) {
			return nil, assert.AnError
		}
		resp := sendWith(t, http.MethodPost, s.url+path, body, authed)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		assert.Equal(t, pluginapi.CodeInternal, decodeAPIError(t, resp).Code)
	})
}
