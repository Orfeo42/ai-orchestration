package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/analytics"
	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
)

var (
	day1 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	day2 = day1.Add(24 * time.Hour)
	day3 = day2.Add(24 * time.Hour)
)

func TestQuerierWorkflows(t *testing.T) {
	t.Run("counts, success rate, percentiles and usage without double counting", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		okStart := day1.Add(time.Hour)
		for _, d := range []time.Duration{10 * time.Second, 20 * time.Second, 30 * time.Second} {
			id := e.addProcess(t, processFixture{
				Workflow: "wf", Status: "succeeded", CreatedAt: okStart, StartedAt: ptr(okStart), EndedAt: ptr(okStart.Add(d)),
			})
			e.addStepRun(t, stepRunFixture{ProcessID: id, StepID: "a", Uses: "agent", Attempt: 1, Status: "succeeded", Input: 1, Output: 2, StartedAt: okStart})
			e.addStepRun(t, stepRunFixture{ProcessID: id, StepID: "b", Uses: "agent", Attempt: 1, Status: "succeeded", Input: 10, Output: 20, StartedAt: okStart})
		}
		e.addProcess(t, processFixture{
			Workflow: "wf", Status: "failed", CreatedAt: okStart, EndedAt: ptr(okStart.Add(40 * time.Second)),
		})
		e.addProcess(t, processFixture{Workflow: "wf", Status: "running", CreatedAt: okStart})
		e.addProcess(t, processFixture{Workflow: "other", Status: "cancelled", CreatedAt: okStart, EndedAt: ptr(okStart.Add(time.Second))})

		got, err := e.q.Workflows(ctx, analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "other", got[0].Workflow)
		assert.Equal(t, 0.0, got[0].SuccessRate)
		wf := got[1]
		assert.Equal(t, "wf", wf.Workflow)
		assert.Equal(t, 5, wf.Processes)
		assert.Equal(t, 1, wf.Active)
		assert.Equal(t, 3, wf.Succeeded)
		assert.Equal(t, 1, wf.Failed)
		assert.Equal(t, 0, wf.Cancelled)
		assert.InDelta(t, 0.75, wf.SuccessRate, 0.0001)
		assert.Equal(t, int64(25000), wf.DurationP50Ms)
		assert.Equal(t, int64(38500), wf.DurationP95Ms)
		assert.Equal(t, events.TokenUsage{Input: 33, Output: 66}, wf.Usage)
	})

	t.Run("range is half-open on created_at", func(t *testing.T) {
		e := newEnv(t)
		e.addProcess(t, processFixture{Workflow: "at-from", Status: "pending", CreatedAt: day1})
		e.addProcess(t, processFixture{Workflow: "at-to", Status: "pending", CreatedAt: day2})

		got, err := e.q.Workflows(context.Background(), analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "at-from", got[0].Workflow)
	})

	t.Run("empty range returns empty non-nil slice", func(t *testing.T) {
		e := newEnv(t)

		got, err := e.q.Workflows(context.Background(), analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
}

func TestQuerierSteps(t *testing.T) {
	t.Run("outcomes, failure rate, retries, percentiles and usage", func(t *testing.T) {
		e := newEnv(t)
		start := day1.Add(time.Hour)
		id := e.addProcess(t, processFixture{Workflow: "wf", Status: "running", CreatedAt: start})
		e.addStepRun(t, stepRunFixture{ProcessID: id, StepID: "s1", Uses: "agent", Attempt: 1, Status: "succeeded", Outcome: "done", Input: 1, Output: 1, StartedAt: start, EndedAt: ptr(start.Add(10 * time.Second))})
		e.addStepRun(t, stepRunFixture{ProcessID: id, StepID: "s1", Uses: "agent", Attempt: 1, Status: "succeeded", Outcome: "done", Input: 1, Output: 1, StartedAt: start, EndedAt: ptr(start.Add(20 * time.Second))})
		e.addStepRun(t, stepRunFixture{ProcessID: id, StepID: "s1", Uses: "agent", Attempt: 2, Status: "failed", Input: 1, Output: 1, StartedAt: start, EndedAt: ptr(start.Add(30 * time.Second))})
		e.addStepRun(t, stepRunFixture{ProcessID: id, StepID: "s1", Uses: "agent", Attempt: 1, Status: "cancelled", Input: 1, Output: 1, StartedAt: start})
		e.addStepRun(t, stepRunFixture{ProcessID: id, StepID: "s0", Uses: "git", Attempt: 1, Status: "succeeded", Outcome: "ok", StartedAt: start})

		got, err := e.q.Steps(context.Background(), "wf", analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "s0", got[0].StepID)
		assert.Equal(t, map[string]int{"ok": 1}, got[0].Outcomes)
		s1 := got[1]
		assert.Equal(t, "wf", s1.Workflow)
		assert.Equal(t, "s1", s1.StepID)
		assert.Equal(t, "agent", s1.Uses)
		assert.Equal(t, 4, s1.Runs)
		assert.Equal(t, map[string]int{"done": 2, "failed": 1, "cancelled": 1}, s1.Outcomes)
		assert.InDelta(t, 0.25, s1.FailureRate, 0.0001)
		assert.Equal(t, 1, s1.Retries)
		assert.Equal(t, int64(20000), s1.DurationP50Ms)
		assert.Equal(t, int64(29000), s1.DurationP95Ms)
		assert.Equal(t, events.TokenUsage{Input: 4, Output: 4}, s1.Usage)
	})

	t.Run("filters by workflow and range on started_at", func(t *testing.T) {
		e := newEnv(t)
		mine := e.addProcess(t, processFixture{Workflow: "wf", Status: "running", CreatedAt: day1})
		other := e.addProcess(t, processFixture{Workflow: "other", Status: "running", CreatedAt: day1})
		e.addStepRun(t, stepRunFixture{ProcessID: mine, StepID: "in-from", Uses: "agent", Attempt: 1, Status: "succeeded", StartedAt: day1})
		e.addStepRun(t, stepRunFixture{ProcessID: mine, StepID: "at-to", Uses: "agent", Attempt: 1, Status: "succeeded", StartedAt: day2})
		e.addStepRun(t, stepRunFixture{ProcessID: other, StepID: "foreign", Uses: "agent", Attempt: 1, Status: "succeeded", StartedAt: day1})

		got, err := e.q.Steps(context.Background(), "wf", analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "in-from", got[0].StepID)
	})

	t.Run("empty range returns empty non-nil slice", func(t *testing.T) {
		e := newEnv(t)

		got, err := e.q.Steps(context.Background(), "wf", analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
}

func TestQuerierTokens(t *testing.T) {
	seed := func(t *testing.T, e *env) {
		a := e.addProcess(t, processFixture{Workflow: "wf", Project: "proj-a", Status: "running", CreatedAt: day1})
		b := e.addProcess(t, processFixture{Workflow: "wf", Project: "proj-b", Status: "running", CreatedAt: day1})
		e.addStepRun(t, stepRunFixture{ProcessID: a, StepID: "s1", Uses: "agent", Attempt: 1, Status: "succeeded", Input: 1, Output: 2, StartedAt: day1.Add(time.Hour)})
		e.addStepRun(t, stepRunFixture{ProcessID: a, StepID: "s2", Uses: "git", Attempt: 1, Status: "succeeded", Input: 10, Output: 20, StartedAt: day1.Add(2 * time.Hour)})
		e.addStepRun(t, stepRunFixture{ProcessID: b, StepID: "s1", Uses: "agent", Attempt: 1, Status: "succeeded", Input: 100, Output: 200, StartedAt: day2.Add(time.Hour)})
		e.addStepRun(t, stepRunFixture{ProcessID: b, StepID: "s1", Uses: "agent", Attempt: 1, Status: "succeeded", Input: 1000, Output: 2000, StartedAt: day3})
	}
	r := analytics.Range{From: day1, To: day3}

	t.Run("groups by day", func(t *testing.T) {
		e := newEnv(t)
		seed(t, e)

		got, err := e.q.Tokens(context.Background(), r, analytics.GroupDay)

		require.NoError(t, err)
		assert.Equal(t, []analytics.TokenPoint{
			{Bucket: "2026-10-01", Usage: events.TokenUsage{Input: 11, Output: 22}},
			{Bucket: "2026-10-02", Usage: events.TokenUsage{Input: 100, Output: 200}},
		}, got)
	})

	t.Run("groups by project", func(t *testing.T) {
		e := newEnv(t)
		seed(t, e)

		got, err := e.q.Tokens(context.Background(), r, analytics.GroupProject)

		require.NoError(t, err)
		assert.Equal(t, []analytics.TokenPoint{
			{Bucket: "proj-a", Usage: events.TokenUsage{Input: 11, Output: 22}},
			{Bucket: "proj-b", Usage: events.TokenUsage{Input: 100, Output: 200}},
		}, got)
	})

	t.Run("groups by step uses", func(t *testing.T) {
		e := newEnv(t)
		seed(t, e)

		got, err := e.q.Tokens(context.Background(), r, analytics.GroupStep)

		require.NoError(t, err)
		assert.Equal(t, []analytics.TokenPoint{
			{Bucket: "agent", Usage: events.TokenUsage{Input: 101, Output: 202}},
			{Bucket: "git", Usage: events.TokenUsage{Input: 10, Output: 20}},
		}, got)
	})

	t.Run("unknown grouping returns error", func(t *testing.T) {
		e := newEnv(t)

		_, err := e.q.Tokens(context.Background(), r, analytics.Grouping("nope"))

		require.ErrorIs(t, err, analytics.ErrInvalidGrouping)
	})

	t.Run("empty range returns empty non-nil slice", func(t *testing.T) {
		e := newEnv(t)

		got, err := e.q.Tokens(context.Background(), r, analytics.GroupDay)

		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
}

func TestQuerierFailures(t *testing.T) {
	t.Run("groups by kind and error ordered by count then recency", func(t *testing.T) {
		e := newEnv(t)
		end := day1.Add(time.Hour)
		e.addProcess(t, processFixture{Workflow: "wf", Status: "failed", Error: "boom", ErrorKind: "internal", CreatedAt: day1, EndedAt: ptr(end)})
		e.addProcess(t, processFixture{Workflow: "wf", Status: "failed", Error: "boom", ErrorKind: "internal", CreatedAt: day1, EndedAt: ptr(end.Add(time.Hour))})
		e.addProcess(t, processFixture{Workflow: "wf", Status: "failed", Error: "boom", ErrorKind: "upstream", CreatedAt: day1, EndedAt: ptr(end)})
		e.addProcess(t, processFixture{Workflow: "wf", Status: "failed", Error: "late", ErrorKind: "business", CreatedAt: day1, EndedAt: ptr(end.Add(2 * time.Hour))})
		e.addProcess(t, processFixture{Workflow: "wf", Status: "succeeded", Error: "ignored", CreatedAt: day1, EndedAt: ptr(end)})

		got, err := e.q.Failures(context.Background(), analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, process.ErrorKindInternal, got[0].ErrorKind)
		assert.Equal(t, "boom", got[0].Error)
		assert.Equal(t, 2, got[0].Count)
		assert.True(t, got[0].LastAt.Equal(end.Add(time.Hour)))
		assert.Equal(t, "late", got[1].Error)
		assert.Equal(t, 1, got[1].Count)
		assert.Equal(t, process.ErrorKindUpstream, got[2].ErrorKind)
	})

	t.Run("range is half-open on ended_at", func(t *testing.T) {
		e := newEnv(t)
		e.addProcess(t, processFixture{Workflow: "wf", Status: "failed", Error: "at-from", ErrorKind: "internal", CreatedAt: day1, EndedAt: ptr(day1)})
		e.addProcess(t, processFixture{Workflow: "wf", Status: "failed", Error: "at-to", ErrorKind: "internal", CreatedAt: day1, EndedAt: ptr(day2)})

		got, err := e.q.Failures(context.Background(), analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "at-from", got[0].Error)
	})

	t.Run("empty range returns empty non-nil slice", func(t *testing.T) {
		e := newEnv(t)

		got, err := e.q.Failures(context.Background(), analytics.Range{From: day1, To: day2})

		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
}
