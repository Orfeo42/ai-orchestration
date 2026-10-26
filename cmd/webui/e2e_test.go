package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Orfeo42/ai-orchestration/internal/db"
	"github.com/Orfeo42/ai-orchestration/internal/engine"
	"github.com/Orfeo42/ai-orchestration/internal/plugins"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
	"github.com/Orfeo42/ai-orchestration/internal/runtime"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/trigger"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
	pluginserver "github.com/Orfeo42/ai-orchestration/pluginapi/server"
)

const (
	e2eTemplate   = "tpl"
	e2ePlugin     = "e2e"
	e2eToken      = "plugin-token"
	e2eDeadline   = 30 * time.Second
	e2eTick       = 50 * time.Millisecond
	e2eHTTPClient = 10 * time.Second
)

var (
	e2eAdminDSN string
	e2eSeq      atomic.Int64
	errFlaky    = errors.New("flaky dependency")
)

func TestMain(m *testing.M) {
	os.Exit(e2eMain(m))
}

func e2eMain(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:18.6-alpine",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres:", err)
		return 1
	}
	code := e2eWithContainer(ctx, m, container)
	if err := container.Terminate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "terminate postgres:", err)
	}
	return code
}

func e2eWithContainer(ctx context.Context, m *testing.M, container *postgres.PostgresContainer) int {
	var err error
	e2eAdminDSN, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dsn:", err)
		return 1
	}
	if err := e2eTemplateDB(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "template:", err)
		return 1
	}
	return m.Run()
}

func e2eTemplateDB(ctx context.Context) error {
	admin, err := sql.Open("pgx", e2eAdminDSN)
	if err != nil {
		return err
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+e2eTemplate); err != nil {
		return err
	}
	if err := admin.Close(); err != nil {
		return err
	}
	d, err := db.Open(ctx, e2eDSN(e2eTemplate))
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, d); err != nil {
		return err
	}
	return d.Close()
}

func e2eDSN(name string) string {
	return strings.Replace(e2eAdminDSN, "/postgres?", "/"+name+"?", 1)
}

type e2eRetry struct{}

func (e2eRetry) NextRetry(*rivertype.JobRow) time.Time {
	return time.Now().Add(20 * time.Millisecond)
}

type e2eMessage struct {
	Message string `json:"message"`
}

type e2eLabel struct {
	Label string `json:"label"`
}

type e2eAction struct {
	spec pluginapi.ActionSpec
	run  func(ctx context.Context, job pluginapi.Job) (pluginapi.Output, error)
}

func (a e2eAction) Spec() pluginapi.ActionSpec { return a.spec }

func (a e2eAction) Run(ctx context.Context, job pluginapi.Job, _ pluginapi.Reporter) (pluginapi.Output, error) {
	return a.run(ctx, job)
}

func e2eSpec(t *testing.T, name string, mode pluginapi.Mode) pluginapi.ActionSpec {
	t.Helper()
	schema, err := pluginapi.SchemaFor[e2eMessage]()
	require.NoError(t, err)
	return pluginapi.ActionSpec{
		Name:                  name,
		Description:           name + " action",
		Mode:                  mode,
		InputSchema:           schema,
		OutputSchema:          schema,
		Outcomes:              []pluginapi.Outcome{pluginapi.OutcomeOK},
		DefaultTimeoutSeconds: 60,
	}
}

func echoOutput(job pluginapi.Job) (pluginapi.Output, error) {
	var in e2eMessage
	if err := json.Unmarshal(job.Input, &in); err != nil {
		return pluginapi.Output{}, err
	}
	data, err := json.Marshal(e2eMessage{Message: "echo: " + in.Message})
	if err != nil {
		return pluginapi.Output{}, err
	}
	return pluginapi.Output{Outcome: pluginapi.OutcomeOK, Data: data, Usage: pluginapi.Usage{Input: 2, Output: 3}}, nil
}

type e2ePluginState struct {
	waitCalls atomic.Int64
	failFirst bool
	block     bool
	cancelled chan string
}

func startE2EPlugin(t *testing.T, st *e2ePluginState) string {
	t.Helper()
	payload, err := pluginapi.SchemaFor[e2eLabel]()
	require.NoError(t, err)
	syncAction := e2eAction{spec: e2eSpec(t, "echo", pluginapi.ModeSync), run: func(_ context.Context, job pluginapi.Job) (pluginapi.Output, error) {
		return echoOutput(job)
	}}
	asyncAction := e2eAction{spec: e2eSpec(t, "wait", pluginapi.ModeAsync), run: func(ctx context.Context, job pluginapi.Job) (pluginapi.Output, error) {
		n := st.waitCalls.Add(1)
		if st.block {
			<-ctx.Done()
			st.cancelled <- job.ID
			return pluginapi.Output{}, ctx.Err()
		}
		if st.failFirst && n == 1 {
			return pluginapi.Output{}, fmt.Errorf("%w: %w", pluginapi.ErrUpstream, errFlaky)
		}
		return echoOutput(job)
	}}
	srv, err := pluginserver.New(pluginserver.Config{Token: e2eToken, RetryBackoff: 20 * time.Millisecond}, pluginapi.Info{
		Name:        e2ePlugin,
		Version:     "1.0.0",
		Description: "end to end test plugin",
		Triggers:    []pluginapi.TriggerSpec{{Name: "ping", Description: "test trigger", PayloadSchema: payload}},
	}, syncAction, asyncAction)
	require.NoError(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, srv.Shutdown(ctx))
		ts.Close()
	})
	return ts.URL
}

type e2eRig struct {
	p    *runtime.Platform
	core string
	hc   *http.Client
}

func newE2ERig(t *testing.T) *e2eRig {
	t.Helper()
	if testing.Short() {
		t.Skip("integration")
	}
	t.Setenv(plugins.TokenEnv(e2ePlugin), e2eToken)
	ctx := context.Background()

	admin, err := sql.Open("pgx", e2eAdminDSN)
	require.NoError(t, err)
	name := fmt.Sprintf("e2e%d_%d", time.Now().UnixNano(), e2eSeq.Add(1))
	_, err = admin.ExecContext(ctx, "CREATE DATABASE "+name+" TEMPLATE "+e2eTemplate)
	require.NoError(t, err)
	require.NoError(t, admin.Close())

	coreTS := httptest.NewUnstartedServer(nil)
	coreURL := "http://" + coreTS.Listener.Addr().String()

	workers := river.NewWorkers()
	p, err := runtime.OpenPlatform(ctx, runtime.PlatformConfig{
		DSN:      e2eDSN(name),
		Registry: step.NewRegistry(),
		Plugins:  runtime.PluginConfig{PublicURL: coreURL, CallbackSecret: "callback-secret"},
		Engine:   engine.Config{WorkerID: "e2e", DefaultTimeout: time.Minute},
		River: &river.Config{
			Queues:            map[string]river.QueueConfig{step.QueueDefault: {MaxWorkers: 2}},
			Workers:           workers,
			FetchPollInterval: 50 * time.Millisecond,
			FetchCooldown:     10 * time.Millisecond,
			RetryPolicy:       e2eRetry{},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, p.Close()) })

	handles := func(ctx context.Context, id string) *runstore.RunHandle {
		return runstore.NewStepHandle(ctx, nil, p.Store, nil, id, runstore.ServiceOrchestrator)
	}
	require.NoError(t, river.AddWorkerSafely(workers, engine.NewStepWorker(p.Engine, p.Queue, handles, engine.Hooks{})))
	require.NoError(t, p.River.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		assert.NoError(t, p.River.StopAndCancel(stopCtx))
	})

	pl := p.Plugins
	plug := &pluginHandler{
		registry:  plugins.NewRegistry(pl.Store, pl.Clients, pl.Catalog, p.Store),
		catalog:   pl.Catalog,
		callbacks: plugins.NewCallbacks(p.Store, pl.Catalog, p.Engine),
		verifier:  pl.Signer,
		auth:      pl.Clients,
		starter:   trigger.New(nil, p.Store, p.Engine, pl.Catalog, trigger.Config{}),
		store:     p.Store,
		codec:     defaultCodec{catalog: pl.Catalog},
	}
	srv, err := newServer("", &processHandler{}, plug)
	require.NoError(t, err)
	coreTS.Config.Handler = srv.srv.Handler
	coreTS.Start()
	t.Cleanup(coreTS.Close)

	return &e2eRig{p: p, core: coreURL, hc: &http.Client{Timeout: e2eHTTPClient}}
}

func (r *e2eRig) post(t *testing.T, path string, body any, header http.Header) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, r.core+path, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := r.hc.Do(req)
	require.NoError(t, err)
	defer func() { assert.NoError(t, resp.Body.Close()) }()
	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, buf.Bytes()
}

func (r *e2eRig) register(t *testing.T, pluginURL string) {
	t.Helper()
	status, body := r.post(t, "/api/plugins", map[string]string{"name": e2ePlugin, "base_url": pluginURL}, nil)
	require.Equal(t, http.StatusCreated, status, string(body))
}

func (r *e2eRig) saveWorkflow(t *testing.T, waitRetry int) {
	t.Helper()
	def := map[string]any{
		"name":    "e2e",
		"trigger": map[string]any{"plugin": e2ePlugin, "event": "ping", "match": map[string]any{"label": "go"}},
		"start":   "say",
		"steps": map[string]any{
			"say": map[string]any{
				"description": "sync echo of the subject",
				"uses":        e2ePlugin + "/echo",
				"with":        map[string]any{"message": "${subject.key}"},
				"on":          map[string]any{"ok": "wait"},
			},
			"wait": map[string]any{
				"uses":  e2ePlugin + "/wait",
				"with":  map[string]any{"message": "${vars.say.message}"},
				"retry": waitRetry,
				"on":    map[string]any{"ok": "end"},
			},
		},
	}
	status, body := r.post(t, "/api/workflows", def, nil)
	require.Equal(t, http.StatusOK, status, string(body))
}

func (r *e2eRig) fire(t *testing.T, label, key string) []string {
	t.Helper()
	header := http.Header{}
	header.Set(pluginapi.HeaderPluginName, e2ePlugin)
	header.Set(pluginapi.HeaderAuthorization, pluginapi.BearerPrefix+e2eToken)
	ev := pluginapi.TriggerEvent{
		Plugin:  e2ePlugin,
		Trigger: "ping",
		Subject: pluginapi.Subject{Kind: "issue", Key: key, Title: "title " + key},
		Payload: json.RawMessage(`{"label":"` + label + `"}`),
		At:      time.Now().UTC(),
	}
	status, body := r.post(t, pluginapi.CorePathTrigger, ev, header)
	require.Equal(t, http.StatusAccepted, status, string(body))
	var accepted pluginapi.TriggerAccepted
	require.NoError(t, json.Unmarshal(body, &accepted))
	return accepted.ProcessIDs
}

func (r *e2eRig) waitStatus(t *testing.T, id string, want process.Status) process.Process {
	t.Helper()
	var last process.Process
	ok := assert.Eventually(t, func() bool {
		p, err := r.p.Store.GetProcess(context.Background(), id)
		if err != nil {
			return false
		}
		last = p
		return p.Status == want
	}, e2eDeadline, e2eTick)
	require.True(t, ok, "process %s last status %s error %q", id, last.Status, last.Error)
	return last
}

func (r *e2eRig) runs(t *testing.T, id string) []process.StepRun {
	t.Helper()
	runs, err := r.p.Store.ListStepRuns(context.Background(), id)
	require.NoError(t, err)
	return runs
}

func TestEndToEndPlugins(t *testing.T) {
	t.Run("trigger starts a process through a sync and an async plugin step", func(t *testing.T) {
		r := newE2ERig(t)
		r.register(t, startE2EPlugin(t, &e2ePluginState{}))
		r.saveWorkflow(t, 0)

		assert.Empty(t, r.fire(t, "other", "o/r#0"))
		ids := r.fire(t, "go", "o/r#1")
		require.Len(t, ids, 1)

		p := r.waitStatus(t, ids[0], process.StatusSucceeded)
		assert.JSONEq(t, `{"label":"go"}`, string(p.Vars["trigger"]))
		assert.JSONEq(t, `{"message":"echo: o/r#1"}`, string(p.Vars["say"]))
		assert.JSONEq(t, `{"message":"echo: echo: o/r#1"}`, string(p.Vars["wait"]))
		runs := r.runs(t, ids[0])
		require.Len(t, runs, 2)
		for _, run := range runs {
			assert.Equal(t, process.StepSucceeded, run.Status)
			assert.Equal(t, e2ePlugin, run.Plugin)
			assert.Equal(t, 2, run.Usage.Input)
		}
	})

	t.Run("async upstream failure is retried and then succeeds", func(t *testing.T) {
		r := newE2ERig(t)
		r.register(t, startE2EPlugin(t, &e2ePluginState{failFirst: true}))
		r.saveWorkflow(t, 1)

		ids := r.fire(t, "go", "o/r#2")
		require.Len(t, ids, 1)

		r.waitStatus(t, ids[0], process.StatusSucceeded)
		runs := r.runs(t, ids[0])
		require.Len(t, runs, 3)
		assert.Equal(t, process.StepFailed, runs[1].Status)
		assert.Equal(t, process.ErrorKindUpstream, runs[1].ErrorKind)
		assert.Equal(t, 1, runs[1].Attempt)
		assert.Equal(t, process.StepSucceeded, runs[2].Status)
		assert.Equal(t, 2, runs[2].Attempt)
	})

	t.Run("async failure without retries fails the process", func(t *testing.T) {
		r := newE2ERig(t)
		r.register(t, startE2EPlugin(t, &e2ePluginState{failFirst: true}))
		r.saveWorkflow(t, 0)

		ids := r.fire(t, "go", "o/r#3")
		require.Len(t, ids, 1)

		p := r.waitStatus(t, ids[0], process.StatusFailed)
		assert.Equal(t, process.ErrorKindUpstream, p.ErrorKind)
	})

	t.Run("cancelling a waiting process cancels the plugin job", func(t *testing.T) {
		r := newE2ERig(t)
		st := &e2ePluginState{block: true, cancelled: make(chan string, 1)}
		r.register(t, startE2EPlugin(t, st))
		r.saveWorkflow(t, 0)

		ids := r.fire(t, "go", "o/r#4")
		require.Len(t, ids, 1)
		r.waitStatus(t, ids[0], process.StatusWaiting)

		require.NoError(t, r.p.Engine.Cancel(context.Background(), ids[0]))

		select {
		case jobID := <-st.cancelled:
			runs := r.runs(t, ids[0])
			require.Len(t, runs, 2)
			assert.Equal(t, runs[1].ID, jobID)
			assert.Equal(t, process.StepCancelled, runs[1].Status)
		case <-time.After(e2eDeadline):
			t.Fatal("plugin job was never cancelled")
		}
		assert.Equal(t, process.StatusCancelled, r.waitStatus(t, ids[0], process.StatusCancelled).Status)
	})
}
