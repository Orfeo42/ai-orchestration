package postgres

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
)

func TestCreateProcess(t *testing.T) {
	t.Run("creates new process", func(t *testing.T) {
		e := newEnv(t)
		p := e.newProcess("wf", "a/b#1")
		p.Project = "proj"
		p.Vars = process.Vars{"k": []byte(`"v"`)}

		created, err := e.store.CreateProcess(context.Background(), p)

		require.NoError(t, err)
		assert.True(t, created)
		got, err := e.store.GetProcess(context.Background(), p.ID)
		require.NoError(t, err)
		assert.Equal(t, "proj", got.Project)
		assert.Equal(t, process.StatusPending, got.Status)
		assert.Equal(t, "a/b#1", got.Subject.Key)
		assert.JSONEq(t, `"v"`, string(got.Vars["k"]))
	})

	t.Run("nil vars are stored as empty object", func(t *testing.T) {
		e := newEnv(t)
		p := e.newProcess("wf", "s1")
		p.Vars = nil

		created, err := e.store.CreateProcess(context.Background(), p)

		require.NoError(t, err)
		assert.True(t, created)
		got, err := e.store.GetProcess(context.Background(), p.ID)
		require.NoError(t, err)
		assert.NotNil(t, got.Vars)
		assert.Empty(t, got.Vars)
	})

	t.Run("second create for same workflow and subject returns false", func(t *testing.T) {
		e := newEnv(t)
		e.seedProcess(t, "wf", "s1")

		created, err := e.store.CreateProcess(context.Background(), e.newProcess("wf", "s1"))

		require.NoError(t, err)
		assert.False(t, created)
	})

	t.Run("second create returns false even after first failed", func(t *testing.T) {
		e := newEnv(t)
		first := e.seedProcess(t, "wf", "s1")
		ok, err := e.store.FinishProcess(context.Background(), first.ID, process.Finish{Status: process.StatusFailed, Error: "boom", ErrorKind: process.ErrorKindInternal})
		require.NoError(t, err)
		require.True(t, ok)

		created, err := e.store.CreateProcess(context.Background(), e.newProcess("wf", "s1"))

		require.NoError(t, err)
		assert.False(t, created)
	})

	t.Run("same subject in another workflow is created", func(t *testing.T) {
		e := newEnv(t)
		e.seedProcess(t, "wf", "s1")

		created, err := e.store.CreateProcess(context.Background(), e.newProcess("other", "s1"))

		require.NoError(t, err)
		assert.True(t, created)
	})

	t.Run("concurrent creates yield exactly one winner", func(t *testing.T) {
		e := newEnv(t)
		var winners atomic.Int64
		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				created, err := e.store.CreateProcess(context.Background(), e.newProcess("wf", "race"))
				assert.NoError(t, err)
				if created {
					winners.Add(1)
				}
			}()
		}
		wg.Wait()

		assert.Equal(t, int64(1), winners.Load())
	})
}

func TestGetProcess(t *testing.T) {
	t.Run("unknown id returns not found", func(t *testing.T) {
		e := newEnv(t)

		_, err := e.store.GetProcess(context.Background(), "00000000-0000-0000-0000-000000000001")

		require.ErrorIs(t, err, process.ErrNotFound)
	})

	t.Run("sums usage across step runs", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 10)
		first := newStepRun(p.ID, 10)
		second := newStepRun(p.ID, 10)
		require.NoError(t, e.store.StartStepRun(ctx, first))
		require.NoError(t, e.store.StartStepRun(ctx, second))
		require.NoError(t, e.store.AddStepUsage(ctx, first.ID, events.TokenUsage{Input: 1, Output: 2, CacheCreationInput: 3, CacheReadInput: 4}))
		require.NoError(t, e.store.AddStepUsage(ctx, second.ID, events.TokenUsage{Input: 10, Output: 20, CacheCreationInput: 30, CacheReadInput: 40}))

		got, err := e.store.GetProcess(ctx, p.ID)

		require.NoError(t, err)
		assert.Equal(t, events.TokenUsage{Input: 11, Output: 22, CacheCreationInput: 33, CacheReadInput: 44}, got.Usage)
		require.NotNil(t, got.CurrentJobID)
		assert.Equal(t, int64(10), *got.CurrentJobID)
		assert.NotNil(t, got.StartedAt)
	})

	t.Run("process without step runs has zero usage", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedProcess(t, "wf", "s1")

		got, err := e.store.GetProcess(context.Background(), p.ID)

		require.NoError(t, err)
		assert.Equal(t, events.TokenUsage{}, got.Usage)
	})
}

func TestLockProcess(t *testing.T) {
	t.Run("returns process inside tx", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedProcess(t, "wf", "s1")

		err := e.store.InTx(context.Background(), func(ctx context.Context, tx process.Tx) error {
			got, err := tx.LockProcess(ctx, p.ID)
			require.NoError(t, err)
			assert.Equal(t, p.ID, got.ID)
			assert.Equal(t, events.TokenUsage{}, got.Usage)
			return nil
		})

		require.NoError(t, err)
	})

	t.Run("unknown id returns not found", func(t *testing.T) {
		e := newEnv(t)

		_, err := e.store.LockProcess(context.Background(), "00000000-0000-0000-0000-000000000001")

		require.ErrorIs(t, err, process.ErrNotFound)
	})
}

func TestListProcesses(t *testing.T) {
	t.Run("filters by status", func(t *testing.T) {
		e := newEnv(t)
		e.seedProcess(t, "wf", "s1")
		running := e.seedRunning(t, "wf", "s2", 1)

		page, err := e.store.ListProcesses(context.Background(), process.ListFilter{Status: process.StatusRunning})

		require.NoError(t, err)
		require.Len(t, page.Processes, 1)
		assert.Equal(t, running.ID, page.Processes[0].ID)
	})

	t.Run("filters by workflow name", func(t *testing.T) {
		e := newEnv(t)
		e.seedProcess(t, "wf", "s1")
		other := e.seedProcess(t, "other", "s2")

		page, err := e.store.ListProcesses(context.Background(), process.ListFilter{WorkflowName: "other"})

		require.NoError(t, err)
		require.Len(t, page.Processes, 1)
		assert.Equal(t, other.ID, page.Processes[0].ID)
	})

	t.Run("filters by project", func(t *testing.T) {
		e := newEnv(t)
		e.seedProcess(t, "wf", "s1")
		withProject := e.newProcess("wf", "s2")
		withProject.Project = "proj"
		created, err := e.store.CreateProcess(context.Background(), withProject)
		require.NoError(t, err)
		require.True(t, created)

		page, err := e.store.ListProcesses(context.Background(), process.ListFilter{Project: "proj"})

		require.NoError(t, err)
		require.Len(t, page.Processes, 1)
		assert.Equal(t, withProject.ID, page.Processes[0].ID)
	})

	t.Run("no filter returns all newest first", func(t *testing.T) {
		e := newEnv(t)
		first := e.seedProcess(t, "wf", "s1")
		second := e.seedProcess(t, "wf", "s2")

		page, err := e.store.ListProcesses(context.Background(), process.ListFilter{})

		require.NoError(t, err)
		require.Len(t, page.Processes, 2)
		assert.Equal(t, second.ID, page.Processes[0].ID)
		assert.Equal(t, first.ID, page.Processes[1].ID)
		assert.Nil(t, page.Next)
	})

	t.Run("keyset paging walks all pages without duplicates", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		for _, subject := range []string{"s1", "s2", "s3", "s4", "s5"} {
			e.seedProcess(t, "wf", subject)
		}

		page1, err := e.store.ListProcesses(ctx, process.ListFilter{Limit: 2})
		require.NoError(t, err)
		require.Len(t, page1.Processes, 2)
		require.NotNil(t, page1.Next)

		page2, err := e.store.ListProcesses(ctx, process.ListFilter{Limit: 2, Cursor: page1.Next})
		require.NoError(t, err)
		require.Len(t, page2.Processes, 2)
		require.NotNil(t, page2.Next)

		page3, err := e.store.ListProcesses(ctx, process.ListFilter{Limit: 2, Cursor: page2.Next})
		require.NoError(t, err)
		require.Len(t, page3.Processes, 1)
		assert.Nil(t, page3.Next)

		seen := map[string]bool{}
		for _, p := range append(append(page1.Processes, page2.Processes...), page3.Processes...) {
			assert.False(t, seen[p.ID])
			seen[p.ID] = true
		}
		assert.Len(t, seen, 5)
	})

	t.Run("includes aggregated usage", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 5)
		run := newStepRun(p.ID, 5)
		require.NoError(t, e.store.StartStepRun(ctx, run))
		require.NoError(t, e.store.AddStepUsage(ctx, run.ID, events.TokenUsage{Input: 7}))

		page, err := e.store.ListProcesses(ctx, process.ListFilter{})

		require.NoError(t, err)
		require.Len(t, page.Processes, 1)
		assert.Equal(t, 7, page.Processes[0].Usage.Input)
	})
}

func TestListWaiting(t *testing.T) {
	t.Run("returns only waiting processes of the kind", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		comment := e.seedRunning(t, "wf", "s1", 1)
		signal := e.seedRunning(t, "wf", "s2", 2)
		e.seedRunning(t, "wf", "s3", 3)
		ok, err := e.store.SetWaiting(ctx, comment.ID, process.Wait{Kind: process.WaitComment, Step: "s1", Since: time.Now().UTC(), ResumeOutcome: "go"})
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = e.store.SetWaiting(ctx, signal.ID, process.Wait{Kind: process.WaitSignal, Step: "s1", Since: time.Now().UTC()})
		require.NoError(t, err)
		require.True(t, ok)

		got, err := e.store.ListWaiting(ctx, process.WaitComment)

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, comment.ID, got[0].ID)
		require.NotNil(t, got[0].WaitingFor)
		assert.Equal(t, "go", got[0].WaitingFor.ResumeOutcome)
	})

	t.Run("none waiting returns empty", func(t *testing.T) {
		e := newEnv(t)

		got, err := e.store.ListWaiting(context.Background(), process.WaitComment)

		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestSetNext(t *testing.T) {
	t.Run("from allowed status moves to running", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedProcess(t, "wf", "s1")

		ok, err := e.store.SetNext(context.Background(), p.ID, process.Next{From: []process.Status{process.StatusPending}, Step: "build", JobID: 42})

		require.NoError(t, err)
		assert.True(t, ok)
		got, err := e.store.GetProcess(context.Background(), p.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StatusRunning, got.Status)
		assert.Equal(t, "build", got.CurrentStep)
		require.NotNil(t, got.CurrentJobID)
		assert.Equal(t, int64(42), *got.CurrentJobID)
	})

	t.Run("from wrong status returns false", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedProcess(t, "wf", "s1")

		ok, err := e.store.SetNext(context.Background(), p.ID, process.Next{From: []process.Status{process.StatusWaiting}, Step: "build", JobID: 42})

		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("retry from failed clears error and end time", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedProcess(t, "wf", "s1")
		_, err := e.store.FinishProcess(ctx, p.ID, process.Finish{Status: process.StatusFailed, Error: "boom", ErrorKind: process.ErrorKindUpstream})
		require.NoError(t, err)

		ok, err := e.store.SetNext(ctx, p.ID, process.Next{From: process.RetryableStatuses(), Step: "s1", JobID: 9})

		require.NoError(t, err)
		assert.True(t, ok)
		got, err := e.store.GetProcess(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StatusRunning, got.Status)
		assert.Empty(t, got.Error)
		assert.Equal(t, process.ErrorKindNone, got.ErrorKind)
		assert.Nil(t, got.EndedAt)
	})
}

func TestSetWaiting(t *testing.T) {
	t.Run("running process becomes waiting", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 1)

		ok, err := e.store.SetWaiting(ctx, p.ID, process.Wait{Kind: process.WaitSignal, Step: "s1", Since: time.Now().UTC()})

		require.NoError(t, err)
		assert.True(t, ok)
		got, err := e.store.GetProcess(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StatusWaiting, got.Status)
		assert.Nil(t, got.CurrentJobID)
		require.NotNil(t, got.WaitingFor)
		assert.Equal(t, process.WaitSignal, got.WaitingFor.Kind)
	})

	t.Run("non running process returns false", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedProcess(t, "wf", "s1")

		ok, err := e.store.SetWaiting(context.Background(), p.ID, process.Wait{Kind: process.WaitSignal, Step: "s1"})

		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestFinishProcess(t *testing.T) {
	t.Run("active process is finished", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 1)

		ok, err := e.store.FinishProcess(ctx, p.ID, process.Finish{Status: process.StatusFailed, Error: "boom", ErrorKind: process.ErrorKindBusiness})

		require.NoError(t, err)
		assert.True(t, ok)
		got, err := e.store.GetProcess(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StatusFailed, got.Status)
		assert.Equal(t, "boom", got.Error)
		assert.Equal(t, process.ErrorKindBusiness, got.ErrorKind)
		assert.Equal(t, "s1", got.CurrentStep)
		assert.Nil(t, got.CurrentJobID)
		assert.NotNil(t, got.EndedAt)
	})

	t.Run("terminal process is not overridden", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 1)
		_, err := e.store.FinishProcess(ctx, p.ID, process.Finish{Status: process.StatusSucceeded})
		require.NoError(t, err)

		ok, err := e.store.FinishProcess(ctx, p.ID, process.Finish{Status: process.StatusFailed, Error: "late"})

		require.NoError(t, err)
		assert.False(t, ok)
		got, err := e.store.GetProcess(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StatusSucceeded, got.Status)
		assert.Empty(t, got.Error)
	})
}

func TestMergeVars(t *testing.T) {
	t.Run("merges new keys and overwrites existing", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.newProcess("wf", "s1")
		p.Vars = process.Vars{"a": []byte(`1`), "b": []byte(`2`)}
		created, err := e.store.CreateProcess(ctx, p)
		require.NoError(t, err)
		require.True(t, created)

		require.NoError(t, e.store.MergeVars(ctx, p.ID, process.Vars{"b": []byte(`3`), "c": []byte(`4`)}))

		got, err := e.store.GetProcess(ctx, p.ID)
		require.NoError(t, err)
		assert.JSONEq(t, `1`, string(got.Vars["a"]))
		assert.JSONEq(t, `3`, string(got.Vars["b"]))
		assert.JSONEq(t, `4`, string(got.Vars["c"]))
	})

	t.Run("nil vars keeps existing vars", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.newProcess("wf", "s1")
		p.Vars = process.Vars{"a": []byte(`1`)}
		created, err := e.store.CreateProcess(ctx, p)
		require.NoError(t, err)
		require.True(t, created)

		require.NoError(t, e.store.MergeVars(ctx, p.ID, nil))
		require.NoError(t, e.store.MergeVars(ctx, p.ID, process.Vars{}))

		got, err := e.store.GetProcess(ctx, p.ID)
		require.NoError(t, err)
		assert.JSONEq(t, `1`, string(got.Vars["a"]))
	})

	t.Run("unknown process returns not found", func(t *testing.T) {
		e := newEnv(t)

		err := e.store.MergeVars(context.Background(), "00000000-0000-0000-0000-000000000001", process.Vars{"a": []byte(`1`)})

		require.ErrorIs(t, err, process.ErrNotFound)
	})
}

func TestInTx(t *testing.T) {
	t.Run("error rolls back writes", func(t *testing.T) {
		e := newEnv(t)
		p := e.newProcess("wf", "s1")
		boom := assert.AnError

		err := e.store.InTx(context.Background(), func(ctx context.Context, tx process.Tx) error {
			created, err := tx.CreateProcess(ctx, p)
			require.NoError(t, err)
			require.True(t, created)
			return boom
		})

		require.ErrorIs(t, err, boom)
		_, err = e.store.GetProcess(context.Background(), p.ID)
		require.ErrorIs(t, err, process.ErrNotFound)
	})

	t.Run("success commits writes and exposes sql tx", func(t *testing.T) {
		e := newEnv(t)
		p := e.newProcess("wf", "s1")

		err := e.store.InTx(context.Background(), func(ctx context.Context, tx process.Tx) error {
			assert.NotNil(t, tx.SQL())
			_, err := tx.CreateProcess(ctx, p)
			return err
		})

		require.NoError(t, err)
		_, err = e.store.GetProcess(context.Background(), p.ID)
		require.NoError(t, err)
	})
}
