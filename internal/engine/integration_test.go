package engine_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
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
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
	"github.com/Orfeo42/ai-orchestration/internal/runtime"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
)

const (
	templateName = "tpl"
	pollDeadline = 30 * time.Second
	pollTick     = 25 * time.Millisecond
)

var (
	adminDSN string
	dbSeq    atomic.Int64

	errBoom = errors.New("boom")
)

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
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
	code := runWithContainer(ctx, m, container)
	if err := container.Terminate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "terminate postgres:", err)
	}
	return code
}

func runWithContainer(ctx context.Context, m *testing.M, container *postgres.PostgresContainer) int {
	var err error
	adminDSN, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dsn:", err)
		return 1
	}
	if err := prepareTemplate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "template:", err)
		return 1
	}
	return m.Run()
}

func prepareTemplate(ctx context.Context) error {
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return err
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+templateName); err != nil {
		return err
	}
	if err := admin.Close(); err != nil {
		return err
	}
	d, err := db.Open(ctx, dsnFor(templateName))
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, d); err != nil {
		return err
	}
	return d.Close()
}

func dsnFor(name string) string {
	return strings.Replace(adminDSN, "/postgres?", "/"+name+"?", 1)
}

type fastRetry struct{}

func (fastRetry) NextRetry(*rivertype.JobRow) time.Time {
	return time.Now().Add(20 * time.Millisecond)
}

type fakeStep struct {
	run func(ctx context.Context, sc step.Context) (step.Result, error)
}

func (f fakeStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	return f.run(ctx, sc)
}

type fake struct {
	def step.Def
	run func(ctx context.Context, sc step.Context) (step.Result, error)
}

type cancelControl struct {
	mu      sync.Mutex
	handles map[string]*runstore.RunHandle
}

func (c *cancelControl) Cancel(_ context.Context, runID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h, ok := c.handles[runID]; ok {
		h.Cancel()
	}
	return nil
}

func (c *cancelControl) handle(ctx context.Context, id string) *runstore.RunHandle {
	h := runstore.NewStepHandle(ctx, nil, nil, nil, id, runstore.ServiceOrchestrator)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handles[id] = h
	return h
}

type rig struct {
	p         *runtime.Platform
	versionID int64
}

func newRig(t *testing.T, def workflow.Definition, fakes []fake) *rig {
	t.Helper()
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()

	admin, err := sql.Open("pgx", adminDSN)
	require.NoError(t, err)
	name := fmt.Sprintf("t%d_%d", time.Now().UnixNano(), dbSeq.Add(1))
	_, err = admin.ExecContext(ctx, "CREATE DATABASE "+name+" TEMPLATE "+templateName)
	require.NoError(t, err)
	require.NoError(t, admin.Close())

	reg := step.NewRegistry()
	for _, f := range fakes {
		run := f.run
		require.NoError(t, step.Register(reg, f.def, func(step.NoParams) step.Step { return fakeStep{run: run} }))
	}

	ctrl := &cancelControl{handles: map[string]*runstore.RunHandle{}}
	workers := river.NewWorkers()
	p, err := runtime.OpenPlatform(ctx, runtime.PlatformConfig{
		DSN:      dsnFor(name),
		Registry: reg,
		Control:  ctrl,
		Engine:   engine.Config{WorkerID: "w1", DefaultTimeout: time.Minute},
		River: &river.Config{
			Queues: map[string]river.QueueConfig{
				step.QueueClaude:  {MaxWorkers: 1},
				step.QueueDefault: {MaxWorkers: 2},
			},
			Workers:           workers,
			FetchPollInterval: 50 * time.Millisecond,
			FetchCooldown:     10 * time.Millisecond,
			RetryPolicy:       fastRetry{},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, p.Close()) })

	require.NoError(t, river.AddWorkerSafely(workers, engine.NewStepWorker(p.Engine, p.Queue, ctrl.handle, engine.Hooks{})))
	require.NoError(t, p.River.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		assert.NoError(t, p.River.StopAndCancel(stopCtx))
	})

	raw, err := json.Marshal(def)
	require.NoError(t, err)
	version, err := p.Store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{
		Name: def.Name, Hash: workflow.Hash(raw), Definition: raw, SourceYAML: "name: " + def.Name,
	})
	require.NoError(t, err)
	return &rig{p: p, versionID: version.ID}
}

func (r *rig) start(t *testing.T, key string) process.Process {
	t.Helper()
	res, err := r.p.Engine.Start(context.Background(), engine.StartRequest{
		WorkflowVersionID: r.versionID,
		Subject:           process.Subject{Kind: process.SubjectIssue, Key: key},
		Title:             "title " + key,
		Vars:              process.Vars{},
	})
	require.NoError(t, err)
	require.True(t, res.Created)
	return res.Process
}

func (r *rig) waitStatus(t *testing.T, id string, want process.Status) process.Process {
	t.Helper()
	var last process.Process
	ok := assert.Eventually(t, func() bool {
		p, err := r.p.Store.GetProcess(context.Background(), id)
		if err != nil {
			return false
		}
		last = p
		return p.Status == want
	}, pollDeadline, pollTick, "process %s never reached %s", id, want)
	require.True(t, ok, "last status %s", last.Status)
	return last
}

func (r *rig) stepRuns(t *testing.T, id string, n int) []process.StepRun {
	t.Helper()
	var runs []process.StepRun
	ok := assert.Eventually(t, func() bool {
		got, err := r.p.Store.ListStepRuns(context.Background(), id)
		if err != nil {
			return false
		}
		runs = got
		for _, run := range got {
			if run.Status == process.StepRunning {
				return false
			}
		}
		return len(got) == n
	}, pollDeadline, pollTick, "expected %d settled step runs", n)
	require.True(t, ok)
	return runs
}

func okDef(name string, outcomes ...step.Outcome) step.Def {
	return step.Def{Name: name, Queue: step.QueueDefault, Outcomes: outcomes}
}

func TestIntegration(t *testing.T) {
	t.Run("linear workflow succeeds and passes vars between steps", func(t *testing.T) {
		def := workflow.Definition{
			Name:  "linear",
			Start: "a",
			Steps: map[string]workflow.StepDef{
				"a": {Uses: "fake.a", On: map[step.Outcome]string{step.OutcomeOK: "b"}},
				"b": {Uses: "fake.b", On: map[step.Outcome]string{step.OutcomeOK: workflow.TargetEnd}},
			},
		}
		var seen json.RawMessage
		r := newRig(t, def, []fake{
			{def: okDef("fake.a", step.OutcomeOK), run: func(context.Context, step.Context) (step.Result, error) {
				return step.Result{Outcome: step.OutcomeOK, Vars: process.Vars{"greeting": json.RawMessage(`"hi"`)}}, nil
			}},
			{def: step.Def{Name: "fake.b", Queue: step.QueueClaude, Outcomes: []step.Outcome{step.OutcomeOK}}, run: func(_ context.Context, sc step.Context) (step.Result, error) {
				seen = sc.Process.Vars["greeting"]
				return step.Result{Outcome: step.OutcomeOK}, nil
			}},
		})

		p := r.start(t, "o/r#1")
		done := r.waitStatus(t, p.ID, process.StatusSucceeded)
		runs := r.stepRuns(t, p.ID, 2)

		assert.JSONEq(t, `"hi"`, string(seen))
		assert.JSONEq(t, `"hi"`, string(done.Vars["greeting"]))
		assert.Equal(t, "a", runs[0].StepID)
		assert.Equal(t, "b", runs[1].StepID)
		assert.Equal(t, process.StepSucceeded, runs[0].Status)
		assert.Equal(t, process.StepSucceeded, runs[1].Status)
	})

	t.Run("signal wait parks the process until signalled", func(t *testing.T) {
		def := workflow.Definition{
			Name:  "signal",
			Start: "a",
			Steps: map[string]workflow.StepDef{
				"a": {Uses: "fake.wait", On: map[step.Outcome]string{"approved": workflow.TargetEnd}},
			},
		}
		r := newRig(t, def, []fake{
			{def: okDef("fake.wait", "approved"), run: func(context.Context, step.Context) (step.Result, error) {
				return step.Result{Wait: &step.WaitRequest{Kind: process.WaitSignal}}, nil
			}},
		})

		p := r.start(t, "o/r#2")
		waiting := r.waitStatus(t, p.ID, process.StatusWaiting)
		require.NotNil(t, waiting.WaitingFor)
		assert.Equal(t, process.WaitSignal, waiting.WaitingFor.Kind)

		require.NoError(t, r.p.Engine.Signal(context.Background(), p.ID, "approved"))
		r.waitStatus(t, p.ID, process.StatusSucceeded)
	})

	t.Run("failing step with retry fails the process after two attempts", func(t *testing.T) {
		def := workflow.Definition{
			Name:  "failing",
			Start: "a",
			Steps: map[string]workflow.StepDef{
				"a": {Uses: "fake.fail", Retry: 1, On: map[step.Outcome]string{step.OutcomeOK: workflow.TargetEnd}},
			},
		}
		r := newRig(t, def, []fake{
			{def: okDef("fake.fail", step.OutcomeOK), run: func(context.Context, step.Context) (step.Result, error) {
				return step.Result{}, errBoom
			}},
		})

		p := r.start(t, "o/r#3")
		failed := r.waitStatus(t, p.ID, process.StatusFailed)
		runs := r.stepRuns(t, p.ID, 2)

		assert.Equal(t, process.ErrorKindInternal, failed.ErrorKind)
		assert.Equal(t, 1, runs[0].Attempt)
		assert.Equal(t, process.StepFailed, runs[0].Status)
		assert.Equal(t, 2, runs[1].Attempt)
		assert.Equal(t, process.StepFailed, runs[1].Status)
	})

	t.Run("cancel stops a blocked step", func(t *testing.T) {
		def := workflow.Definition{
			Name:  "cancel",
			Start: "a",
			Steps: map[string]workflow.StepDef{
				"a": {Uses: "fake.block", On: map[step.Outcome]string{step.OutcomeOK: workflow.TargetEnd}},
			},
		}
		started := make(chan struct{})
		r := newRig(t, def, []fake{
			{def: okDef("fake.block", step.OutcomeOK), run: func(ctx context.Context, _ step.Context) (step.Result, error) {
				close(started)
				<-ctx.Done()
				return step.Result{}, ctx.Err()
			}},
		})

		p := r.start(t, "o/r#4")
		select {
		case <-started:
		case <-time.After(pollDeadline):
			require.FailNow(t, "step never started")
		}

		require.NoError(t, r.p.Engine.Cancel(context.Background(), p.ID))
		r.waitStatus(t, p.ID, process.StatusCancelled)
		runs := r.stepRuns(t, p.ID, 1)
		assert.Equal(t, process.StepCancelled, runs[0].Status)
	})

	t.Run("starting the same subject twice creates one process", func(t *testing.T) {
		def := workflow.Definition{
			Name:  "dedup",
			Start: "a",
			Steps: map[string]workflow.StepDef{
				"a": {Uses: "fake.wait", On: map[step.Outcome]string{"approved": workflow.TargetEnd}},
			},
		}
		r := newRig(t, def, []fake{
			{def: okDef("fake.wait", "approved"), run: func(context.Context, step.Context) (step.Result, error) {
				return step.Result{Wait: &step.WaitRequest{Kind: process.WaitSignal}}, nil
			}},
		})

		r.start(t, "o/r#5")
		second, err := r.p.Engine.Start(context.Background(), engine.StartRequest{
			WorkflowVersionID: r.versionID,
			Subject:           process.Subject{Kind: process.SubjectIssue, Key: "o/r#5"},
			Title:             "again",
			Vars:              process.Vars{},
		})
		require.NoError(t, err)
		assert.False(t, second.Created)
	})

	t.Run("async job wait completes via CompleteJob and advances", func(t *testing.T) {
		def := workflow.Definition{
			Name:  "async",
			Start: "a",
			Steps: map[string]workflow.StepDef{
				"a": {Uses: "fake.async", On: map[step.Outcome]string{step.OutcomeOK: "b"}},
				"b": {Uses: "fake.b", On: map[step.Outcome]string{step.OutcomeOK: workflow.TargetEnd}},
			},
		}
		var seen json.RawMessage
		r := newRig(t, def, []fake{
			{def: okDef("fake.async", step.OutcomeOK), run: asyncRun},
			{def: okDef("fake.b", step.OutcomeOK), run: func(_ context.Context, sc step.Context) (step.Result, error) {
				seen = sc.Process.Vars["a"]
				return step.Result{Outcome: step.OutcomeOK}, nil
			}},
		})

		p := r.start(t, "o/r#6")
		waiting := r.waitStatus(t, p.ID, process.StatusWaiting)
		require.NotNil(t, waiting.WaitingFor)
		assert.Equal(t, process.WaitJob, waiting.WaitingFor.Kind)
		require.NotNil(t, waiting.WaitingFor.Job)
		run := r.runningRun(t, p.ID, 1)
		assert.Equal(t, run.ID, waiting.WaitingFor.Job.StepRunID)

		require.NoError(t, r.p.Engine.CompleteJob(context.Background(), engine.JobResult{
			StepRunID: run.ID,
			Outcome:   step.OutcomeOK,
			Output:    json.RawMessage(`{"answer":42}`),
		}))

		r.waitStatus(t, p.ID, process.StatusSucceeded)
		runs := r.stepRuns(t, p.ID, 2)
		assert.Equal(t, process.StepSucceeded, runs[0].Status)
		assert.Equal(t, "ok", runs[0].Outcome)
		assert.JSONEq(t, `{"answer":42}`, string(seen))
	})

	t.Run("async job error is retried and records attempt two", func(t *testing.T) {
		def := workflow.Definition{
			Name:  "asyncretry",
			Start: "a",
			Steps: map[string]workflow.StepDef{
				"a": {Uses: "fake.async", Retry: 1, On: map[step.Outcome]string{step.OutcomeOK: workflow.TargetEnd}},
			},
		}
		r := newRig(t, def, []fake{
			{def: okDef("fake.async", step.OutcomeOK), run: asyncRun},
		})

		p := r.start(t, "o/r#7")
		r.waitStatus(t, p.ID, process.StatusWaiting)
		first := r.runningRun(t, p.ID, 1)

		require.NoError(t, r.p.Engine.CompleteJob(context.Background(), engine.JobResult{
			StepRunID: first.ID,
			Error:     "plugin failed",
			ErrorKind: process.ErrorKindUpstream,
		}))

		second := r.runningRun(t, p.ID, 2)
		assert.NotEqual(t, first.ID, second.ID)
		assert.Equal(t, 2, second.Attempt)
		require.NoError(t, r.p.Engine.CompleteJob(context.Background(), engine.JobResult{StepRunID: second.ID, Outcome: step.OutcomeOK}))

		r.waitStatus(t, p.ID, process.StatusSucceeded)
		runs := r.stepRuns(t, p.ID, 2)
		assert.Equal(t, process.StepFailed, runs[0].Status)
		assert.Equal(t, process.ErrorKindUpstream, runs[0].ErrorKind)
		assert.Equal(t, 1, runs[0].Attempt)
		assert.Equal(t, process.StepSucceeded, runs[1].Status)
		assert.Equal(t, 2, runs[1].Attempt)
	})

	t.Run("async job error on last attempt fails the process", func(t *testing.T) {
		def := workflow.Definition{
			Name:  "asyncfail",
			Start: "a",
			Steps: map[string]workflow.StepDef{
				"a": {Uses: "fake.async", On: map[step.Outcome]string{step.OutcomeOK: workflow.TargetEnd}},
			},
		}
		r := newRig(t, def, []fake{
			{def: okDef("fake.async", step.OutcomeOK), run: asyncRun},
		})

		p := r.start(t, "o/r#8")
		r.waitStatus(t, p.ID, process.StatusWaiting)
		run := r.runningRun(t, p.ID, 1)

		require.NoError(t, r.p.Engine.CompleteJob(context.Background(), engine.JobResult{
			StepRunID: run.ID,
			Error:     "plugin failed",
			ErrorKind: process.ErrorKindUpstream,
		}))

		failed := r.waitStatus(t, p.ID, process.StatusFailed)
		assert.Equal(t, process.ErrorKindUpstream, failed.ErrorKind)
		assert.Equal(t, "plugin failed", failed.Error)
	})
}

func asyncRun(context.Context, step.Context) (step.Result, error) {
	return step.Result{Wait: &step.WaitRequest{
		Kind: process.WaitJob,
		Job:  &step.JobWait{Plugin: "pl", HeartbeatSeconds: 30, Deadline: time.Now().Add(time.Hour)},
	}}, nil
}

func (r *rig) runningRun(t *testing.T, id string, n int) process.StepRun {
	t.Helper()
	var running process.StepRun
	ok := assert.Eventually(t, func() bool {
		got, err := r.p.Store.ListStepRuns(context.Background(), id)
		if err != nil || len(got) != n {
			return false
		}
		last := got[len(got)-1]
		running = last
		return last.Status == process.StepRunning
	}, pollDeadline, pollTick, "expected %d step runs with the last running", n)
	require.True(t, ok)
	return running
}
