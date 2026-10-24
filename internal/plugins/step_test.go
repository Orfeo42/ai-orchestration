package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/template"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

func stepContext() step.Context {
	return step.Context{
		Process: process.Process{
			ID:           "p1",
			WorkflowName: "wf",
			Title:        "title",
			Project:      "proj",
			Subject:      process.Subject{Kind: process.SubjectIssue, Key: "42", URL: "http://issue"},
			Vars:         process.Vars{"count": json.RawMessage(`3`)},
		},
		StepID:    "s1",
		StepRunID: "run-1",
	}
}

func stepBuild(t *testing.T, catalog *Catalog, with string) step.Step {
	t.Helper()
	s, err := catalog.Build("alpha/act", json.RawMessage(with))
	require.NoError(t, err)
	return s
}

func stepOKOutput() pluginapi.Output {
	return pluginapi.Output{Outcome: pluginapi.OutcomeOK, Data: json.RawMessage(`{"v":1}`), Usage: pluginapi.Usage{Input: 1}}
}

func stepFixtureFor(t *testing.T, url string, m pluginapi.Manifest) *Catalog {
	t.Helper()
	catalog, _, _ := catFixture(t, catTokens, catRecord("alpha", url, m))
	return catalog
}

func stepFixtureSync(t *testing.T, out pluginapi.Output, runErr error) *Catalog {
	t.Helper()
	ts, m := catServe(t, "alpha", catSyncAction("act", out, runErr))
	return stepFixtureFor(t, ts.URL, m)
}

func TestNewStep(t *testing.T) {
	m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
	catalog := stepFixtureFor(t, "http://alpha", m)
	rec, _ := catalog.Plugin("alpha")
	spec := catSpec("act", pluginapi.ModeSync)

	t.Run("schema-valid with", func(t *testing.T) {
		s, err := newStep(catalog, rec, spec, json.RawMessage(`{"n":1}`))
		require.NoError(t, err)
		assert.NotNil(t, s)
	})

	t.Run("invalid with wraps ErrInvalidParams", func(t *testing.T) {
		_, err := newStep(catalog, rec, spec, json.RawMessage(`{"n":"x"}`))
		require.ErrorIs(t, err, step.ErrInvalidParams)
	})

	t.Run("empty with is treated as empty object and fails required", func(t *testing.T) {
		_, err := newStep(catalog, rec, spec, nil)
		require.ErrorIs(t, err, step.ErrInvalidParams)
	})

	t.Run("with expressions skips schema validation", func(t *testing.T) {
		s, err := newStep(catalog, rec, spec, json.RawMessage(`{"n":"${vars.count}"}`))
		require.NoError(t, err)
		assert.NotNil(t, s)
	})

	t.Run("with expressions must be an object", func(t *testing.T) {
		_, err := newStep(catalog, rec, spec, json.RawMessage(`"${vars.count}"`))
		require.ErrorIs(t, err, step.ErrInvalidParams)
	})

	t.Run("invalid input schema wraps ErrInvalidParams", func(t *testing.T) {
		bad := spec
		bad.InputSchema = json.RawMessage(`{"type":`)
		_, err := newStep(catalog, rec, bad, json.RawMessage(`{"n":1}`))
		require.ErrorIs(t, err, step.ErrInvalidParams)
	})
}

func TestStepRun(t *testing.T) {
	t.Run("sync success stores output under the step id", func(t *testing.T) {
		catalog := stepFixtureSync(t, stepOKOutput(), nil)
		res, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
		assert.Nil(t, res.Wait)
		assert.JSONEq(t, `{"v":1}`, string(res.Vars["s1"]))
	})

	t.Run("expressions are resolved from the scope", func(t *testing.T) {
		var got json.RawMessage
		action := stepAction{
			spec: catSpec("act", pluginapi.ModeSync),
			run: func(_ context.Context, job pluginapi.Job) (pluginapi.Output, error) {
				got = job.Input
				return stepOKOutput(), nil
			},
		}
		ts, m := catServe(t, "alpha", action)
		catalog := stepFixtureFor(t, ts.URL, m)

		_, err := stepBuild(t, catalog, `{"n":"${vars.count}","label":"${subject.key}-${process.workflow}"}`).Run(context.Background(), stepContext())
		require.NoError(t, err)
		assert.JSONEq(t, `{"n":3,"label":"42-wf"}`, string(got))
	})

	t.Run("sync failed upstream wraps ErrUpstream", func(t *testing.T) {
		catalog := stepFixtureSync(t, pluginapi.Output{}, fmt.Errorf("%w: boom", pluginapi.ErrUpstream))
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrUpstream)
		assert.ErrorContains(t, err, "boom")
	})

	t.Run("sync failed internal wraps ErrJobFailed", func(t *testing.T) {
		catalog := stepFixtureSync(t, pluginapi.Output{}, errors.New("boom"))
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, ErrJobFailed)
		assert.NotErrorIs(t, err, step.ErrUpstream)
		assert.ErrorContains(t, err, "boom")
	})

	t.Run("undeclared outcome wraps ErrPermanent", func(t *testing.T) {
		body := `{"job_id":"run-1","action":"act","status":"succeeded","result":{"job_id":"run-1","outcome":"bogus","output":{"v":1}}}`
		ts := catStub(t, 200, body)
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		catalog := stepFixtureFor(t, ts.URL, m)
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrPermanent)
		require.ErrorIs(t, err, ErrInvalidResult)
	})

	t.Run("terminal state without result wraps ErrUpstream", func(t *testing.T) {
		ts := catStub(t, 200, `{"job_id":"run-1","action":"act","status":"failed"}`)
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		catalog := stepFixtureFor(t, ts.URL, m)
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("async returns a job wait with deadline from the step timeout", func(t *testing.T) {
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		ts, m := catServe(t, "alpha", catBlockingAction("act", release))
		catalog := stepFixtureFor(t, ts.URL, m)

		sc := stepContext()
		sc.Timeout = 90 * time.Second
		before := time.Now().UTC()
		res, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), sc)
		require.NoError(t, err)
		require.NotNil(t, res.Wait)
		assert.Equal(t, process.WaitJob, res.Wait.Kind)
		require.NotNil(t, res.Wait.Job)
		assert.Equal(t, "alpha", res.Wait.Job.Plugin)
		assert.Equal(t, DefaultHeartbeatSeconds, res.Wait.Job.HeartbeatSeconds)
		assert.WithinDuration(t, before.Add(90*time.Second), res.Wait.Job.Deadline, 5*time.Second)
		assert.Empty(t, res.Outcome)
		assert.Nil(t, res.Vars)
	})

	t.Run("async deadline falls back to the action default timeout", func(t *testing.T) {
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		ts, m := catServe(t, "alpha", catBlockingAction("act", release))
		catalog := stepFixtureFor(t, ts.URL, m)

		before := time.Now().UTC()
		res, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.NoError(t, err)
		require.NotNil(t, res.Wait)
		assert.WithinDuration(t, before.Add(60*time.Second), res.Wait.Job.Deadline, 5*time.Second)
	})

	t.Run("unresolved expression wraps ErrPermanent", func(t *testing.T) {
		catalog := stepFixtureSync(t, stepOKOutput(), nil)
		_, err := stepBuild(t, catalog, `{"n":"${vars.missing}"}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrPermanent)
		require.ErrorIs(t, err, template.ErrUnresolved)
	})

	t.Run("resolved input violating the schema wraps ErrPermanent", func(t *testing.T) {
		catalog := stepFixtureSync(t, stepOKOutput(), nil)
		_, err := stepBuild(t, catalog, `{"n":"${subject.key}"}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrPermanent)
		require.ErrorIs(t, err, pluginapi.ErrSchemaViolation)
	})

	t.Run("plugin rejecting input with 422 wraps ErrPermanent", func(t *testing.T) {
		ts := catStub(t, 422, `{"code":"invalid_input","message":"bad"}`)
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		catalog := stepFixtureFor(t, ts.URL, m)
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrPermanent)
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("plugin 500 wraps ErrUpstream", func(t *testing.T) {
		ts := catStub(t, 500, `{"code":"internal","message":"oops"}`)
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		catalog := stepFixtureFor(t, ts.URL, m)
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("unreachable plugin wraps ErrUpstream", func(t *testing.T) {
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		catalog := stepFixtureFor(t, catClosedURL(t), m)
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("plugin down wraps ErrUpstream", func(t *testing.T) {
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		rec := catRecord("alpha", "http://alpha", m)
		rec.Status = StatusDown
		catalog, _, _ := catFixture(t, catTokens, rec)
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("plugin unreachable status wraps ErrUpstream", func(t *testing.T) {
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		rec := catRecord("alpha", "http://alpha", m)
		rec.Status = StatusUnreachable
		catalog, _, _ := catFixture(t, catTokens, rec)
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("plugin removed after build wraps ErrUpstream", func(t *testing.T) {
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		records := []Record{catRecord("alpha", "http://alpha", m)}
		store := &MockStore{ListPluginsFunc: func(context.Context) ([]Record, error) { return records, nil }}
		clients := NewClients(catTokens, nil)
		catalog := NewCatalog(step.NewRegistry(), store, clients, NewSigner([]byte("k"), "http://core"))
		clients.Bind(catalog)
		require.NoError(t, catalog.Refresh(context.Background()))
		s := stepBuild(t, catalog, `{"n":1}`)

		records = nil
		require.NoError(t, catalog.Refresh(context.Background()))
		_, err := s.Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("missing token wraps ErrPermanent", func(t *testing.T) {
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		catalog, _, _ := catFixture(t, catNoTokens, catRecord("alpha", "http://alpha", m))
		_, err := stepBuild(t, catalog, `{"n":1}`).Run(context.Background(), stepContext())
		require.ErrorIs(t, err, step.ErrPermanent)
		require.ErrorIs(t, err, ErrNoToken)
	})
}
