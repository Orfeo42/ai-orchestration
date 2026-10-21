package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/analytics"
	"ai-orchestration/internal/control"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/step"
	"ai-orchestration/internal/steps"
	"ai-orchestration/internal/trigger"
	"ai-orchestration/internal/workflow"
)

type processTestServer struct {
	testServer
	reader  *MockProcessReader
	engine  *MockProcessController
	starter *MockProcessStarter
	stats   *analytics.MockQuerier
	events  *MockEventReader
	ctrl    *control.MockChannel
	fj      *forgejo.MockForgejo
	url     string
}

func newProcessTestServer(t *testing.T) processTestServer {
	t.Helper()
	reader := &MockProcessReader{}
	engine := &MockProcessController{}
	starter := &MockProcessStarter{}
	stats := &analytics.MockQuerier{}
	evts := &MockEventReader{}
	ctrl := &control.MockChannel{}
	fj := &forgejo.MockForgejo{}
	base := newTestServerWith(t, &processHandler{
		reader:  reader,
		engine:  engine,
		starter: starter,
		stats:   stats,
		events:  evts,
		ctrl:    ctrl,
		fj:      fj,
	})
	return processTestServer{testServer: base, reader: reader, engine: engine, starter: starter, stats: stats, events: evts, ctrl: ctrl, fj: fj, url: base.ts.URL}
}

func send(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(body))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var out T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

func TestProcessRoutesNotRegisteredWithoutAPI(t *testing.T) {
	t.Run("process route falls through to static handler", func(t *testing.T) {
		s := newTestServer(t)
		resp := send(t, http.MethodPost, s.ts.URL+"/api/processes", `{}`)
		assert.NotEqual(t, http.StatusCreated, resp.StatusCode)
	})
}

func TestListProcesses(t *testing.T) {
	t.Run("returns page with encoded next cursor", func(t *testing.T) {
		s := newProcessTestServer(t)
		next := process.Cursor{CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), ID: "p9"}
		s.reader.ListProcessesFunc = func(_ context.Context, f process.ListFilter) (process.Page, error) {
			assert.Equal(t, process.StatusFailed, f.Status)
			assert.Equal(t, "impl", f.WorkflowName)
			assert.Equal(t, "billing", f.Project)
			assert.Equal(t, 10, f.Limit)
			assert.Nil(t, f.Cursor)
			return process.Page{Processes: []process.Process{{ID: "p1"}}, Next: &next}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/processes?status=failed&workflow=impl&project=billing&limit=10", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[processPageResponse](t, resp)
		require.Len(t, body.Processes, 1)
		got, err := decodeCursor(body.NextCursor)
		require.NoError(t, err)
		assert.Equal(t, next, got)
	})

	t.Run("passes decoded cursor to the reader", func(t *testing.T) {
		s := newProcessTestServer(t)
		cur := process.Cursor{CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), ID: "p9"}
		enc, err := encodeCursor(cur)
		require.NoError(t, err)
		s.reader.ListProcessesFunc = func(_ context.Context, f process.ListFilter) (process.Page, error) {
			require.NotNil(t, f.Cursor)
			assert.Equal(t, cur, *f.Cursor)
			return process.Page{}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/processes?cursor="+enc, "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[processPageResponse](t, resp)
		assert.NotNil(t, body.Processes)
		assert.Empty(t, body.NextCursor)
	})

	t.Run("invalid status returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/processes?status=bogus", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Empty(t, s.reader.ListProcessesCalls())
	})

	t.Run("invalid cursor returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/processes?cursor=!!!", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("valid base64 but not json cursor returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/processes?cursor=aGVsbG8", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("reader failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.ListProcessesFunc = func(context.Context, process.ListFilter) (process.Page, error) {
			return process.Page{}, errors.New("db down")
		}
		resp := send(t, http.MethodGet, s.url+"/api/processes", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestGetProcess(t *testing.T) {
	t.Run("returns process step runs and workflow", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetProcessFunc = func(_ context.Context, id string) (process.Process, error) {
			assert.Equal(t, "p1", id)
			return process.Process{ID: "p1", WorkflowVersionID: 7}, nil
		}
		s.reader.ListStepRunsFunc = func(context.Context, string) ([]process.StepRun, error) {
			return []process.StepRun{{ID: "sr1", ProcessID: "p1"}}, nil
		}
		s.reader.GetWorkflowVersionFunc = func(_ context.Context, id int64) (process.WorkflowVersion, error) {
			assert.Equal(t, int64(7), id)
			return process.WorkflowVersion{ID: 7, Name: "impl", Definition: json.RawMessage(`{"name":"impl"}`)}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/processes/p1", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[processDetailResponse](t, resp)
		assert.Equal(t, "p1", body.Process.ID)
		require.Len(t, body.StepRuns, 1)
		assert.JSONEq(t, `{"name":"impl"}`, string(body.Workflow.Definition))
	})

	t.Run("unknown process returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{}, process.ErrNotFound
		}
		resp := send(t, http.MethodGet, s.url+"/api/processes/nope", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("step run listing failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{ID: "p1"}, nil
		}
		s.reader.ListStepRunsFunc = func(context.Context, string) ([]process.StepRun, error) {
			return nil, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/processes/p1", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})

	t.Run("workflow lookup failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{ID: "p1"}, nil
		}
		s.reader.ListStepRunsFunc = func(context.Context, string) ([]process.StepRun, error) {
			return nil, nil
		}
		s.reader.GetWorkflowVersionFunc = func(context.Context, int64) (process.WorkflowVersion, error) {
			return process.WorkflowVersion{}, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/processes/p1", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestStartProcess(t *testing.T) {
	t.Run("creates process and returns 201", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.starter.StartForIssueFunc = func(_ context.Context, wf, key string) (process.Process, error) {
			assert.Equal(t, "impl", wf)
			assert.Equal(t, "acme/x#5", key)
			return process.Process{ID: "p1", WorkflowName: wf}, nil
		}
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{"workflow":"impl","subject_key":"acme/x#5"}`)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Equal(t, "p1", decode[process.Process](t, resp).ID)
	})

	t.Run("empty fields return 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{"workflow":"impl"}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Empty(t, s.starter.StartForIssueCalls())
	})

	t.Run("malformed body returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unknown workflow step maps to 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.starter.StartForIssueFunc = func(context.Context, string, string) (process.Process, error) {
			return process.Process{}, workflow.ErrUnknownStep
		}
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{"workflow":"impl","subject_key":"a/b#1"}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid subject maps to 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.starter.StartForIssueFunc = func(context.Context, string, string) (process.Process, error) {
			return process.Process{}, trigger.ErrInvalidSubject
		}
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{"workflow":"impl","subject_key":"bad"}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("no project maps to 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.starter.StartForIssueFunc = func(context.Context, string, string) (process.Process, error) {
			return process.Process{}, trigger.ErrNoProject
		}
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{"workflow":"impl","subject_key":"a/b#1"}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unknown workflow maps to 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.starter.StartForIssueFunc = func(context.Context, string, string) (process.Process, error) {
			return process.Process{}, trigger.ErrUnknownWorkflow
		}
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{"workflow":"nope","subject_key":"a/b#1"}`)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("process exists maps to 409", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.starter.StartForIssueFunc = func(context.Context, string, string) (process.Process, error) {
			return process.Process{}, trigger.ErrProcessExists
		}
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{"workflow":"impl","subject_key":"a/b#1"}`)
		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})

	t.Run("unexpected failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.starter.StartForIssueFunc = func(context.Context, string, string) (process.Process, error) {
			return process.Process{}, errors.New("boom")
		}
		resp := send(t, http.MethodPost, s.url+"/api/processes", `{"workflow":"impl","subject_key":"a/b#1"}`)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestCancelProcess(t *testing.T) {
	t.Run("returns 204", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.CancelFunc = func(context.Context, string) error { return nil }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/cancel", "")
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		require.Len(t, s.engine.CancelCalls(), 1)
		assert.Equal(t, "p1", s.engine.CancelCalls()[0].ProcessID)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.CancelFunc = func(context.Context, string) error { return process.ErrNotFound }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/cancel", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("not active returns 409", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.CancelFunc = func(context.Context, string) error { return process.ErrNotActive }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/cancel", "")
		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})

	t.Run("wrapped error is still mapped", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.CancelFunc = func(context.Context, string) error { return errors.Join(errors.New("ctx"), process.ErrNotActive) }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/cancel", "")
		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})

	t.Run("unexpected failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.CancelFunc = func(context.Context, string) error { return errors.New("boom") }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/cancel", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestRetryProcess(t *testing.T) {
	t.Run("passes step from body", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.RetryFunc = func(context.Context, string, string) error { return nil }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/retry", `{"step":"review"}`)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		require.Len(t, s.engine.RetryCalls(), 1)
		assert.Equal(t, "p1", s.engine.RetryCalls()[0].ProcessID)
		assert.Equal(t, "review", s.engine.RetryCalls()[0].StepID)
	})

	t.Run("empty body retries with empty step", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.RetryFunc = func(context.Context, string, string) error { return nil }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/retry", "")
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		assert.Empty(t, s.engine.RetryCalls()[0].StepID)
	})

	t.Run("malformed body returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/retry", `{`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unknown step returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.RetryFunc = func(context.Context, string, string) error { return workflow.ErrUnknownStep }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/retry", `{"step":"zzz"}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.RetryFunc = func(context.Context, string, string) error { return process.ErrNotFound }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/retry", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("not active returns 409", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.RetryFunc = func(context.Context, string, string) error { return process.ErrNotActive }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/retry", "")
		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})
}

func TestSignalProcess(t *testing.T) {
	t.Run("returns 204 and passes outcome", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.SignalFunc = func(context.Context, string, string) error { return nil }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/signal", `{"outcome":"approved"}`)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		assert.Equal(t, "approved", s.engine.SignalCalls()[0].Outcome)
	})

	t.Run("missing outcome returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/signal", `{}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Empty(t, s.engine.SignalCalls())
	})

	t.Run("malformed body returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/signal", `{`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unhandled outcome returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.SignalFunc = func(context.Context, string, string) error { return workflow.ErrUnhandledResult }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/signal", `{"outcome":"x"}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unknown step error returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.SignalFunc = func(context.Context, string, string) error { return step.ErrUnknownStep }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/signal", `{"outcome":"x"}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.SignalFunc = func(context.Context, string, string) error { return process.ErrNotFound }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/signal", `{"outcome":"x"}`)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("not active returns 409", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.engine.SignalFunc = func(context.Context, string, string) error { return process.ErrNotActive }
		resp := send(t, http.MethodPost, s.url+"/api/processes/p1/signal", `{"outcome":"x"}`)
		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})
}

func TestGetStepRun(t *testing.T) {
	t.Run("returns step run", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ID: "sr1", StepID: "impl"}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "sr1", decode[process.StepRun](t, resp).ID)
	})

	t.Run("unknown returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{}, process.ErrNotFound
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/nope", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{}, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestGetStepRunEvents(t *testing.T) {
	t.Run("returns events with since and limit", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.events.EventsRangeFunc = func(_ context.Context, id string, since int64, limit int) ([]events.Event, error) {
			assert.Equal(t, "sr1", id)
			assert.Equal(t, int64(3), since)
			assert.Equal(t, 20, limit)
			return []events.Event{{Seq: 4, Text: "hi"}}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1/events?since=3&limit=20", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[map[string][]events.Event](t, resp)
		require.Len(t, body["events"], 1)
		assert.Equal(t, "hi", body["events"][0].Text)
	})

	t.Run("store failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.events.EventsRangeFunc = func(context.Context, string, int64, int) ([]events.Event, error) {
			return nil, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1/events", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestCancelStepRun(t *testing.T) {
	t.Run("cancels by step run id", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.ctrl.CancelFunc = func(context.Context, string) error { return nil }
		resp := send(t, http.MethodPost, s.url+"/api/step-runs/sr1/cancel", "")
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		assert.Equal(t, "sr1", s.ctrl.CancelCalls()[0].RunID)
	})

	t.Run("channel failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.ctrl.CancelFunc = func(context.Context, string) error { return errors.New("boom") }
		resp := send(t, http.MethodPost, s.url+"/api/step-runs/sr1/cancel", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestInjectStepRun(t *testing.T) {
	t.Run("injects note", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.ctrl.InjectFunc = func(context.Context, string, string) error { return nil }
		resp := send(t, http.MethodPost, s.url+"/api/step-runs/sr1/inject", `{"note":"focus"}`)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		assert.Equal(t, "sr1", s.ctrl.InjectCalls()[0].RunID)
		assert.Equal(t, "focus", s.ctrl.InjectCalls()[0].Note)
	})

	t.Run("empty note returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/step-runs/sr1/inject", `{"note":""}`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Empty(t, s.ctrl.InjectCalls())
	})

	t.Run("malformed body returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodPost, s.url+"/api/step-runs/sr1/inject", `{`)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("channel failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.ctrl.InjectFunc = func(context.Context, string, string) error { return errors.New("boom") }
		resp := send(t, http.MethodPost, s.url+"/api/step-runs/sr1/inject", `{"note":"x"}`)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func prVars(t *testing.T, number int) process.Vars {
	t.Helper()
	vars := process.Vars{}
	require.NoError(t, process.Set(vars, steps.KeyPR, models.ForgejoPR{Number: number, RepoOwner: "acme", RepoName: "code"}))
	return vars
}

func TestGetStepRunDiff(t *testing.T) {
	const rawDiff = "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-a\n+b\n"

	t.Run("returns parsed diff for the process PR", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ID: "sr1", ProcessID: "p1"}, nil
		}
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{ID: "p1", Subject: process.Subject{Key: "acme/issues#5"}, Vars: prVars(t, 12)}, nil
		}
		s.fj.GetPRDiffFunc = func(owner, repo string, n int) (string, error) {
			assert.Equal(t, "acme", owner)
			assert.Equal(t, "code", repo)
			assert.Equal(t, 12, n)
			return rawDiff, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1/diff", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[diffResponse](t, resp)
		assert.Equal(t, 12, body.PRNumber)
		require.Len(t, body.Files, 1)
		assert.Equal(t, "foo.go", body.Files[0].Path)
	})

	t.Run("process without PR returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ProcessID: "p1"}, nil
		}
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{ID: "p1", Subject: process.Subject{Key: "acme/x#5"}, Vars: process.Vars{}}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1/diff", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		assert.Empty(t, s.fj.GetPRDiffCalls())
	})

	t.Run("unknown step run returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{}, process.ErrNotFound
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1/diff", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("unknown process returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ProcessID: "p1"}, nil
		}
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{}, process.ErrNotFound
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1/diff", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("PR var without repo returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ProcessID: "p1"}, nil
		}
		vars := process.Vars{}
		require.NoError(t, process.Set(vars, steps.KeyPR, models.ForgejoPR{Number: 1}))
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{Subject: process.Subject{Key: "acme/x#5"}, Vars: vars}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1/diff", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		assert.Empty(t, s.fj.GetPRDiffCalls())
	})

	t.Run("forgejo failure returns 502", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ProcessID: "p1"}, nil
		}
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{Subject: process.Subject{Key: "acme/x#5"}, Vars: prVars(t, 1)}, nil
		}
		s.fj.GetPRDiffFunc = func(string, string, int) (string, error) { return "", errors.New("boom") }
		resp := send(t, http.MethodGet, s.url+"/api/step-runs/sr1/diff", "")
		assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	})
}

func TestListWorkflows(t *testing.T) {
	t.Run("returns latest versions", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.LatestWorkflowVersionsFunc = func(context.Context) ([]process.WorkflowVersion, error) {
			return []process.WorkflowVersion{{ID: 1, Name: "impl", Definition: json.RawMessage(`{}`)}}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/workflows", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[map[string][]process.WorkflowVersion](t, resp)
		require.Len(t, body["workflows"], 1)
		assert.Equal(t, "impl", body["workflows"][0].Name)
	})

	t.Run("failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.LatestWorkflowVersionsFunc = func(context.Context) ([]process.WorkflowVersion, error) {
			return nil, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/workflows", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestGetWorkflow(t *testing.T) {
	t.Run("returns versions by name", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.ListWorkflowVersionsFunc = func(_ context.Context, name string) ([]process.WorkflowVersion, error) {
			assert.Equal(t, "impl", name)
			return []process.WorkflowVersion{{ID: 2, Definition: json.RawMessage(`{}`)}, {ID: 1, Definition: json.RawMessage(`{}`)}}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/workflows/impl", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Len(t, decode[map[string][]process.WorkflowVersion](t, resp)["versions"], 2)
	})

	t.Run("unknown workflow returns 404", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.reader.ListWorkflowVersionsFunc = func(context.Context, string) ([]process.WorkflowVersion, error) {
			return nil, process.ErrNotFound
		}
		resp := send(t, http.MethodGet, s.url+"/api/workflows/nope", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestAnalyticsWorkflows(t *testing.T) {
	t.Run("date-only to covers the whole day", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.WorkflowsFunc = func(_ context.Context, r analytics.Range) ([]analytics.WorkflowStats, error) {
			assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), r.From)
			assert.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), r.To)
			return []analytics.WorkflowStats{{Workflow: "impl"}}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/workflows?from=2026-01-01&to=2026-01-31", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body := decode[map[string][]analytics.WorkflowStats](t, resp)
		assert.Equal(t, "impl", body["workflows"][0].Workflow)
	})

	t.Run("accepts RFC3339 range", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.WorkflowsFunc = func(_ context.Context, r analytics.Range) ([]analytics.WorkflowStats, error) {
			assert.Equal(t, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), r.From)
			assert.Equal(t, time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), r.To)
			return nil, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/workflows?from=2026-01-01T10:00:00Z&to=2026-01-02T10:00:00Z", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NotNil(t, decode[map[string][]analytics.WorkflowStats](t, resp)["workflows"])
	})

	t.Run("defaults to last 30 days", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.WorkflowsFunc = func(_ context.Context, r analytics.Range) ([]analytics.WorkflowStats, error) {
			assert.WithinDuration(t, time.Now().UTC().AddDate(0, 0, -30), r.From, time.Minute)
			assert.WithinDuration(t, time.Now().UTC(), r.To, time.Minute)
			return nil, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/workflows", "")
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("from after to returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/analytics/workflows?from=2026-02-01&to=2026-01-01", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid date returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/analytics/workflows?from=yesterday", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid to returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/analytics/workflows?to=tomorrow", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("querier failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.WorkflowsFunc = func(context.Context, analytics.Range) ([]analytics.WorkflowStats, error) {
			return nil, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/workflows", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestAnalyticsSteps(t *testing.T) {
	t.Run("passes workflow filter", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.StepsFunc = func(_ context.Context, wf string, _ analytics.Range) ([]analytics.StepStats, error) {
			assert.Equal(t, "impl", wf)
			return []analytics.StepStats{{StepID: "review"}}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/steps?workflow=impl", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "review", decode[map[string][]analytics.StepStats](t, resp)["steps"][0].StepID)
	})

	t.Run("bad range returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/analytics/steps?from=x", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("querier failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.StepsFunc = func(context.Context, string, analytics.Range) ([]analytics.StepStats, error) {
			return nil, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/steps", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestAnalyticsTokens(t *testing.T) {
	t.Run("passes grouping", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.TokensFunc = func(_ context.Context, _ analytics.Range, g analytics.Grouping) ([]analytics.TokenPoint, error) {
			assert.Equal(t, analytics.GroupProject, g)
			return []analytics.TokenPoint{{Bucket: "billing"}}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/tokens?group=project", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "billing", decode[map[string][]analytics.TokenPoint](t, resp)["points"][0].Bucket)
	})

	t.Run("bad group returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/analytics/tokens?group=week", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Empty(t, s.stats.TokensCalls())
	})

	t.Run("bad range returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/analytics/tokens?group=day&from=x", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("querier failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.TokensFunc = func(context.Context, analytics.Range, analytics.Grouping) ([]analytics.TokenPoint, error) {
			return nil, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/tokens?group=day", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestAnalyticsFailures(t *testing.T) {
	t.Run("returns failure groups", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.FailuresFunc = func(context.Context, analytics.Range) ([]analytics.FailureGroup, error) {
			return []analytics.FailureGroup{{Error: "boom", Count: 2}}, nil
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/failures", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, 2, decode[map[string][]analytics.FailureGroup](t, resp)["failures"][0].Count)
	})

	t.Run("bad range returns 400", func(t *testing.T) {
		s := newProcessTestServer(t)
		resp := send(t, http.MethodGet, s.url+"/api/analytics/failures?from=2026-02-01&to=2026-01-01", "")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("querier failure returns 500", func(t *testing.T) {
		s := newProcessTestServer(t)
		s.stats.FailuresFunc = func(context.Context, analytics.Range) ([]analytics.FailureGroup, error) {
			return nil, errors.New("boom")
		}
		resp := send(t, http.MethodGet, s.url+"/api/analytics/failures", "")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestWritePRDiffWithoutForgejo(t *testing.T) {
	t.Run("nil client returns 503", func(t *testing.T) {
		s := newProcessTestServer(t)
		h := &processHandler{
			reader: s.reader,
		}
		srvNoFJ := newTestServerWith(t, h)
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ProcessID: "p1"}, nil
		}
		s.reader.GetProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{Subject: process.Subject{Key: "acme/x#5"}, Vars: prVars(t, 1)}, nil
		}
		resp := send(t, http.MethodGet, srvNoFJ.ts.URL+"/api/step-runs/sr1/diff", "")
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})
}
