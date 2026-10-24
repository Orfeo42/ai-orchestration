package trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/engine"
	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
	"github.com/Orfeo42/ai-orchestration/internal/models"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/projects"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/steps"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

const workflowYAML = `name: implement-issue
trigger:
  forgejo: { repos: [ionstream/handbook, ionstream/other], label: To-do, assignee: bot }
start: assess
steps:
  assess: { uses: claude.assess, on: { clear: end, unclear: ask } }
  ask:    { uses: tracker.ask_clarification, on: { answered: assess } }
`

var errBoom = errors.New("boom")

func newRegistry(t *testing.T) *step.Registry {
	t.Helper()
	reg := step.NewRegistry()
	require.NoError(t, steps.RegisterAll(reg, steps.Deps{}))
	return reg
}

func writeWorkflow(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func testConfig() Config {
	return Config{
		ForgejoURL: "https://git.example.com/",
		Projects:   map[string]projects.Config{"billing": {Repo: "ionstream/billing-backend", Label: "repo/billing-BE"}},
		BotUser:    "bot",
	}
}

func versionFor(t *testing.T, id int64, yaml string) process.WorkflowVersion {
	t.Helper()
	dir := t.TempDir()
	writeWorkflow(t, dir, "w.yaml", yaml)
	store := &MockStore{
		UpsertWorkflowVersionFunc: func(_ context.Context, v process.WorkflowVersion) (process.WorkflowVersion, error) {
			v.ID = id
			return v, nil
		},
		SeedWorkflowFunc: func(context.Context, string, int64) (bool, error) { return true, nil },
	}
	svc := New(&forgejo.MockForgejo{}, store, &MockEngine{}, newRegistry(t), testConfig())
	require.NoError(t, svc.SyncWorkflows(t.Context(), dir))
	require.Len(t, store.UpsertWorkflowVersionCalls(), 1)
	v := store.UpsertWorkflowVersionCalls()[0].V
	v.ID = id
	return v
}

func TestSyncWorkflows(t *testing.T) {
	t.Run("upserts and seeds each file", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "a.yaml", workflowYAML)
		writeWorkflow(t, dir, "b.yaml", strings.Replace(workflowYAML, "implement-issue", "second", 1))
		store := &MockStore{
			UpsertWorkflowVersionFunc: func(_ context.Context, v process.WorkflowVersion) (process.WorkflowVersion, error) {
				v.ID = 1
				return v, nil
			},
			SeedWorkflowFunc: func(context.Context, string, int64) (bool, error) { return true, nil },
		}
		svc := New(&forgejo.MockForgejo{}, store, &MockEngine{}, newRegistry(t), testConfig())

		require.NoError(t, svc.SyncWorkflows(t.Context(), dir))

		calls := store.UpsertWorkflowVersionCalls()
		require.Len(t, calls, 2)
		assert.Equal(t, "implement-issue", calls[0].V.Name)
		assert.Equal(t, "second", calls[1].V.Name)
		assert.Equal(t, workflowYAML, calls[0].V.SourceYAML)
		assert.NotEmpty(t, calls[0].V.Hash)
		assert.Contains(t, string(calls[0].V.Definition), "implement-issue")
		seeds := store.SeedWorkflowCalls()
		require.Len(t, seeds, 2)
		assert.Equal(t, "implement-issue", seeds[0].Name)
		assert.Equal(t, int64(1), seeds[0].VersionID)
		assert.Equal(t, "second", seeds[1].Name)
	})

	t.Run("logs import only when seeded", func(t *testing.T) {
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		t.Cleanup(func() { slog.SetDefault(prev) })
		dir := t.TempDir()
		writeWorkflow(t, dir, "a.yaml", workflowYAML)
		seeded := true
		store := &MockStore{
			UpsertWorkflowVersionFunc: func(_ context.Context, v process.WorkflowVersion) (process.WorkflowVersion, error) {
				v.ID = 3
				return v, nil
			},
			SeedWorkflowFunc: func(context.Context, string, int64) (bool, error) { return seeded, nil },
		}
		svc := New(&forgejo.MockForgejo{}, store, &MockEngine{}, newRegistry(t), testConfig())

		require.NoError(t, svc.SyncWorkflows(t.Context(), dir))
		assert.Contains(t, buf.String(), "workflow imported")

		buf.Reset()
		seeded = false
		require.NoError(t, svc.SyncWorkflows(t.Context(), dir))
		assert.NotContains(t, buf.String(), "workflow imported")
		assert.Len(t, store.SeedWorkflowCalls(), 2)
	})

	t.Run("invalid dir returns error", func(t *testing.T) {
		svc := New(&forgejo.MockForgejo{}, &MockStore{}, &MockEngine{}, newRegistry(t), testConfig())

		require.Error(t, svc.SyncWorkflows(t.Context(), filepath.Join(t.TempDir(), "missing")))
	})

	t.Run("upsert error returns error", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "a.yaml", workflowYAML)
		store := &MockStore{
			UpsertWorkflowVersionFunc: func(context.Context, process.WorkflowVersion) (process.WorkflowVersion, error) {
				return process.WorkflowVersion{}, errBoom
			},
		}
		svc := New(&forgejo.MockForgejo{}, store, &MockEngine{}, newRegistry(t), testConfig())

		require.ErrorIs(t, svc.SyncWorkflows(t.Context(), dir), errBoom)
	})

	t.Run("seed error returns error", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "a.yaml", workflowYAML)
		store := &MockStore{
			UpsertWorkflowVersionFunc: func(_ context.Context, v process.WorkflowVersion) (process.WorkflowVersion, error) {
				return v, nil
			},
			SeedWorkflowFunc: func(context.Context, string, int64) (bool, error) { return false, errBoom },
		}
		svc := New(&forgejo.MockForgejo{}, store, &MockEngine{}, newRegistry(t), testConfig())

		require.ErrorIs(t, svc.SyncWorkflows(t.Context(), dir), errBoom)
	})
}

func pollStore(versions ...process.WorkflowVersion) *MockStore {
	return &MockStore{
		LatestWorkflowVersionsFunc: func(context.Context) ([]process.WorkflowVersion, error) { return versions, nil },
	}
}

func createdEngine() *MockEngine {
	return &MockEngine{
		StartFunc: func(_ context.Context, req engine.StartRequest) (engine.StartResult, error) {
			return engine.StartResult{Process: process.Process{ID: "p1", Subject: req.Subject}, Created: true}, nil
		},
	}
}

func TestPoll(t *testing.T) {
	v := versionFor(t, 7, workflowYAML)

	t.Run("starts process with subject url vars and project", func(t *testing.T) {
		var gotAssignee *string
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				gotAssignee = assignee
				if repo != "handbook" {
					return nil, nil
				}
				return []models.ForgejoIssue{{Number: 5, Title: "t", Labels: []string{"repo/billing-BE"}}}, nil
			},
		}
		eng := createdEngine()
		svc := New(fj, pollStore(v), eng, newRegistry(t), testConfig())

		require.NoError(t, svc.Poll(t.Context()))

		require.Len(t, eng.StartCalls(), 1)
		req := eng.StartCalls()[0].Req
		assert.Equal(t, int64(7), req.WorkflowVersionID)
		assert.Equal(t, process.Subject{Kind: process.SubjectIssue, Key: "ionstream/handbook#5", URL: "https://git.example.com/ionstream/handbook/issues/5"}, req.Subject)
		assert.Equal(t, "t", req.Title)
		assert.Equal(t, "billing", req.Project)
		require.NotNil(t, gotAssignee)
		assert.Equal(t, "bot", *gotAssignee)
		issue, ok, err := process.Get(req.Vars, steps.KeyIssue)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "ionstream", issue.RepoOwner)
		assert.Equal(t, "handbook", issue.RepoName)
		assert.Equal(t, 5, issue.Number)
		project, ok, err := process.Get(req.Vars, steps.KeyProject)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "billing", project)
	})

	t.Run("skips done label", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(string, string, string, *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{{Number: 5, Labels: []string{"repo/billing-BE", forgejo.LabelDone}}}, nil
			},
		}
		eng := createdEngine()
		svc := New(fj, pollStore(v), eng, newRegistry(t), testConfig())

		require.NoError(t, svc.Poll(t.Context()))

		assert.Empty(t, eng.StartCalls())
	})

	t.Run("skips issue without project label", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(string, string, string, *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{{Number: 5, Labels: []string{"bug"}}}, nil
			},
		}
		eng := createdEngine()
		svc := New(fj, pollStore(v), eng, newRegistry(t), testConfig())

		require.NoError(t, svc.Poll(t.Context()))

		assert.Empty(t, eng.StartCalls())
	})

	t.Run("continues after repo error", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(_, repo, _ string, _ *string) ([]models.ForgejoIssue, error) {
				if repo == "handbook" {
					return nil, errBoom
				}
				return []models.ForgejoIssue{{Number: 9, Labels: []string{"repo/billing-BE"}}}, nil
			},
		}
		eng := createdEngine()
		svc := New(fj, pollStore(v), eng, newRegistry(t), testConfig())

		require.NoError(t, svc.Poll(t.Context()))

		require.Len(t, eng.StartCalls(), 1)
		assert.Equal(t, "ionstream/other#9", eng.StartCalls()[0].Req.Subject.Key)
	})

	t.Run("continues after engine error", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(_, repo, _ string, _ *string) ([]models.ForgejoIssue, error) {
				if repo != "handbook" {
					return nil, nil
				}
				return []models.ForgejoIssue{
					{Number: 1, Labels: []string{"repo/billing-BE"}},
					{Number: 2, Labels: []string{"repo/billing-BE"}},
				}, nil
			},
		}
		eng := &MockEngine{
			StartFunc: func(context.Context, engine.StartRequest) (engine.StartResult, error) {
				return engine.StartResult{}, errBoom
			},
		}
		svc := New(fj, pollStore(v), eng, newRegistry(t), testConfig())

		require.NoError(t, svc.Poll(t.Context()))

		assert.Len(t, eng.StartCalls(), 2)
	})

	t.Run("skips workflow without forgejo trigger", func(t *testing.T) {
		manual := process.WorkflowVersion{ID: 8, Name: "manual-only", Definition: []byte(`{"name":"manual-only","trigger":{}}`)}
		fj := &forgejo.MockForgejo{}
		svc := New(fj, pollStore(manual), createdEngine(), newRegistry(t), testConfig())

		require.NoError(t, svc.Poll(t.Context()))

		assert.Empty(t, fj.GetIssuesCalls())
	})

	t.Run("latest versions error returns error", func(t *testing.T) {
		store := &MockStore{
			LatestWorkflowVersionsFunc: func(context.Context) ([]process.WorkflowVersion, error) { return nil, errBoom },
		}
		svc := New(&forgejo.MockForgejo{}, store, createdEngine(), newRegistry(t), testConfig())

		require.ErrorIs(t, svc.Poll(t.Context()), errBoom)
	})
}

func TestResumeWaiting(t *testing.T) {
	since := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	waitingProcess := func(t *testing.T) process.Process {
		t.Helper()
		vars := process.Vars{}
		require.NoError(t, process.Set(vars, steps.KeyIssue, models.ForgejoIssue{Number: 5, RepoOwner: "ionstream", RepoName: "handbook"}))
		return process.Process{ID: "p1", Vars: vars, WaitingFor: &process.Wait{Kind: process.WaitComment, Since: since}}
	}

	run := func(t *testing.T, comments []models.IssueComment) *MockEngine {
		t.Helper()
		store := &MockStore{
			ListWaitingFunc: func(context.Context, process.WaitKind) ([]process.Process, error) {
				return []process.Process{waitingProcess(t)}, nil
			},
		}
		fj := &forgejo.MockForgejo{
			GetCommentsFunc: func(string, string, int) ([]models.IssueComment, error) { return comments, nil },
		}
		eng := &MockEngine{ResumeFunc: func(context.Context, string) error { return nil }}
		svc := New(fj, store, eng, newRegistry(t), testConfig())

		require.NoError(t, svc.ResumeWaiting(t.Context()))
		return eng
	}

	t.Run("resumes on newer human comment", func(t *testing.T) {
		eng := run(t, []models.IssueComment{{Author: "alice", UpdatedAt: since.Add(time.Minute)}})

		require.Len(t, eng.ResumeCalls(), 1)
		assert.Equal(t, "p1", eng.ResumeCalls()[0].ProcessID)
	})

	t.Run("does not resume on bot comment", func(t *testing.T) {
		eng := run(t, []models.IssueComment{{Author: "bot", UpdatedAt: since.Add(time.Minute)}})

		assert.Empty(t, eng.ResumeCalls())
	})

	t.Run("does not resume on older comment", func(t *testing.T) {
		eng := run(t, []models.IssueComment{{Author: "alice", UpdatedAt: since.Add(-time.Minute)}})

		assert.Empty(t, eng.ResumeCalls())
	})

	t.Run("comment fetch error is skipped", func(t *testing.T) {
		store := &MockStore{
			ListWaitingFunc: func(context.Context, process.WaitKind) ([]process.Process, error) {
				return []process.Process{waitingProcess(t)}, nil
			},
		}
		fj := &forgejo.MockForgejo{
			GetCommentsFunc: func(string, string, int) ([]models.IssueComment, error) { return nil, errBoom },
		}
		eng := &MockEngine{}
		svc := New(fj, store, eng, newRegistry(t), testConfig())

		require.NoError(t, svc.ResumeWaiting(t.Context()))

		assert.Empty(t, eng.ResumeCalls())
	})

	t.Run("process without issue var is skipped", func(t *testing.T) {
		store := &MockStore{
			ListWaitingFunc: func(context.Context, process.WaitKind) ([]process.Process, error) {
				return []process.Process{{ID: "p2", Vars: process.Vars{}, WaitingFor: &process.Wait{Since: since}}}, nil
			},
		}
		fj := &forgejo.MockForgejo{}
		svc := New(fj, store, &MockEngine{}, newRegistry(t), testConfig())

		require.NoError(t, svc.ResumeWaiting(t.Context()))

		assert.Empty(t, fj.GetCommentsCalls())
	})

	t.Run("resume error is skipped", func(t *testing.T) {
		store := &MockStore{
			ListWaitingFunc: func(context.Context, process.WaitKind) ([]process.Process, error) {
				return []process.Process{waitingProcess(t)}, nil
			},
		}
		fj := &forgejo.MockForgejo{
			GetCommentsFunc: func(string, string, int) ([]models.IssueComment, error) {
				return []models.IssueComment{{Author: "alice", UpdatedAt: since.Add(time.Minute)}}, nil
			},
		}
		eng := &MockEngine{ResumeFunc: func(context.Context, string) error { return errBoom }}
		svc := New(fj, store, eng, newRegistry(t), testConfig())

		require.NoError(t, svc.ResumeWaiting(t.Context()))

		assert.Len(t, eng.ResumeCalls(), 1)
	})

	t.Run("list error returns error", func(t *testing.T) {
		store := &MockStore{
			ListWaitingFunc: func(context.Context, process.WaitKind) ([]process.Process, error) { return nil, errBoom },
		}
		svc := New(&forgejo.MockForgejo{}, store, &MockEngine{}, newRegistry(t), testConfig())

		require.ErrorIs(t, svc.ResumeWaiting(t.Context()), errBoom)
	})
}

func TestStartForIssue(t *testing.T) {
	v := versionFor(t, 7, workflowYAML)
	issueFj := func(labels ...string) *forgejo.MockForgejo {
		return &forgejo.MockForgejo{
			GetIssueFunc: func(string, string, int) (models.ForgejoIssue, error) {
				return models.ForgejoIssue{Number: 5, Title: "t", Labels: labels}, nil
			},
		}
	}

	t.Run("invalid subject key", func(t *testing.T) {
		svc := New(issueFj(), pollStore(v), createdEngine(), newRegistry(t), testConfig())

		_, err := svc.StartForIssue(context.Background(), "implement-issue", "nonsense")

		require.ErrorIs(t, err, ErrInvalidSubject)
		assert.Contains(t, err.Error(), "nonsense")
	})

	t.Run("non numeric issue number", func(t *testing.T) {
		svc := New(issueFj(), pollStore(v), createdEngine(), newRegistry(t), testConfig())

		_, err := svc.StartForIssue(context.Background(), "implement-issue", "ionstream/handbook#x")

		require.ErrorIs(t, err, ErrInvalidSubject)
	})

	t.Run("unknown workflow", func(t *testing.T) {
		svc := New(issueFj(), pollStore(v), createdEngine(), newRegistry(t), testConfig())

		_, err := svc.StartForIssue(context.Background(), "ghost", "ionstream/handbook#5")

		require.ErrorIs(t, err, ErrUnknownWorkflow)
	})

	t.Run("get issue error is upstream", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssueFunc: func(string, string, int) (models.ForgejoIssue, error) { return models.ForgejoIssue{}, errBoom },
		}
		svc := New(fj, pollStore(v), createdEngine(), newRegistry(t), testConfig())

		_, err := svc.StartForIssue(context.Background(), "implement-issue", "ionstream/handbook#5")

		require.ErrorIs(t, err, step.ErrUpstream)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("no project label", func(t *testing.T) {
		svc := New(issueFj("bug"), pollStore(v), createdEngine(), newRegistry(t), testConfig())

		_, err := svc.StartForIssue(context.Background(), "implement-issue", "ionstream/handbook#5")

		require.ErrorIs(t, err, ErrNoProject)
	})

	t.Run("process already exists", func(t *testing.T) {
		eng := &MockEngine{
			StartFunc: func(context.Context, engine.StartRequest) (engine.StartResult, error) {
				return engine.StartResult{Created: false}, nil
			},
		}
		svc := New(issueFj("repo/billing-BE"), pollStore(v), eng, newRegistry(t), testConfig())

		_, err := svc.StartForIssue(context.Background(), "implement-issue", "ionstream/handbook#5")

		require.ErrorIs(t, err, ErrProcessExists)
	})

	t.Run("engine error is returned", func(t *testing.T) {
		eng := &MockEngine{
			StartFunc: func(context.Context, engine.StartRequest) (engine.StartResult, error) {
				return engine.StartResult{}, errBoom
			},
		}
		svc := New(issueFj("repo/billing-BE"), pollStore(v), eng, newRegistry(t), testConfig())

		_, err := svc.StartForIssue(context.Background(), "implement-issue", "ionstream/handbook#5")

		require.ErrorIs(t, err, errBoom)
	})

	t.Run("starts process", func(t *testing.T) {
		eng := createdEngine()
		svc := New(issueFj("repo/billing-BE"), pollStore(v), eng, newRegistry(t), testConfig())

		got, err := svc.StartForIssue(context.Background(), "implement-issue", "ionstream/handbook#5")

		require.NoError(t, err)
		assert.Equal(t, "p1", got.ID)
		require.Len(t, eng.StartCalls(), 1)
		assert.Equal(t, int64(7), eng.StartCalls()[0].Req.WorkflowVersionID)
		assert.Equal(t, "ionstream/handbook#5", eng.StartCalls()[0].Req.Subject.Key)
		assert.Equal(t, "billing", eng.StartCalls()[0].Req.Project)
	})
}

func pluginVersion(id int64, plugin, event, match string) process.WorkflowVersion {
	def := `{"name":"w` + strconv.FormatInt(id, 10) + `","trigger":{"plugin":"` + plugin + `","event":"` + event + `"`
	if match != "" {
		def += `,"match":` + match
	}
	def += `},"start":"a","steps":{}}`
	return process.WorkflowVersion{ID: id, Name: "w" + strconv.FormatInt(id, 10), Definition: json.RawMessage(def)}
}

func pluginEvent(payload string) pluginapi.TriggerEvent {
	return pluginapi.TriggerEvent{
		Plugin:  "git",
		Trigger: "push",
		Subject: pluginapi.Subject{Kind: "repo", Key: "o/r", URL: "https://x/o/r", Title: "T"},
		Payload: json.RawMessage(payload),
	}
}

func TestStartFromEvent(t *testing.T) {
	t.Run("starts matching workflow with subject and trigger var", func(t *testing.T) {
		eng := createdEngine()
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", `{"branch":"main"}`)), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{"branch":"main","sha":"abc"}`))

		require.NoError(t, err)
		assert.Equal(t, []string{"p1"}, ids)
		require.Len(t, eng.StartCalls(), 1)
		req := eng.StartCalls()[0].Req
		assert.Equal(t, int64(4), req.WorkflowVersionID)
		assert.Equal(t, process.Subject{Kind: "repo", Key: "o/r", URL: "https://x/o/r"}, req.Subject)
		assert.Equal(t, "T", req.Title)
		assert.JSONEq(t, `{"branch":"main","sha":"abc"}`, string(req.Vars["trigger"]))
	})

	t.Run("no match value differs", func(t *testing.T) {
		eng := createdEngine()
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", `{"branch":"main"}`)), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{"branch":"dev"}`))

		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
		assert.Empty(t, eng.StartCalls())
	})

	t.Run("nested path match", func(t *testing.T) {
		eng := createdEngine()
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", `{"repo.owner.name":"orfeo"}`)), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{"repo":{"owner":{"name":"orfeo"}}}`))

		require.NoError(t, err)
		assert.Len(t, ids, 1)
	})

	t.Run("equal numbers match", func(t *testing.T) {
		eng := createdEngine()
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", `{"count":1}`)), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{"count":1}`))

		require.NoError(t, err)
		assert.Len(t, ids, 1)
	})

	t.Run("different numbers do not match", func(t *testing.T) {
		eng := createdEngine()
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", `{"count":1}`)), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{"count":2}`))

		require.NoError(t, err)
		assert.Empty(t, ids)
	})

	t.Run("missing path does not match", func(t *testing.T) {
		eng := createdEngine()
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", `{"branch":"main"}`)), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{"other":1}`))

		require.NoError(t, err)
		assert.Empty(t, ids)
		assert.Empty(t, eng.StartCalls())
	})

	t.Run("syntax error key does not match", func(t *testing.T) {
		eng := createdEngine()
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", `{"a}b":1}`)), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{"a":1}`))

		require.NoError(t, err)
		assert.Empty(t, ids)
	})

	t.Run("empty payload treated as empty object", func(t *testing.T) {
		eng := createdEngine()
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", "")), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(""))

		require.NoError(t, err)
		assert.Len(t, ids, 1)
		assert.JSONEq(t, `{}`, string(eng.StartCalls()[0].Req.Vars["trigger"]))
	})

	t.Run("wrong plugin or event is skipped", func(t *testing.T) {
		eng := createdEngine()
		store := pollStore(pluginVersion(4, "other", "push", ""), pluginVersion(5, "git", "tag", ""))
		svc := New(&forgejo.MockForgejo{}, store, eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{}`))

		require.NoError(t, err)
		assert.Empty(t, ids)
		assert.Empty(t, eng.StartCalls())
	})

	t.Run("undecodable definition is skipped", func(t *testing.T) {
		eng := createdEngine()
		bad := process.WorkflowVersion{ID: 1, Name: "bad", Definition: json.RawMessage(`{`)}
		svc := New(&forgejo.MockForgejo{}, pollStore(bad, pluginVersion(4, "git", "push", "")), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{}`))

		require.NoError(t, err)
		assert.Len(t, ids, 1)
	})

	t.Run("existing process is not collected", func(t *testing.T) {
		eng := &MockEngine{
			StartFunc: func(context.Context, engine.StartRequest) (engine.StartResult, error) {
				return engine.StartResult{}, nil
			},
		}
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", "")), eng, newRegistry(t), testConfig())

		ids, err := svc.StartFromEvent(t.Context(), pluginEvent(`{}`))

		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("engine error is returned", func(t *testing.T) {
		eng := &MockEngine{
			StartFunc: func(context.Context, engine.StartRequest) (engine.StartResult, error) {
				return engine.StartResult{}, errBoom
			},
		}
		svc := New(&forgejo.MockForgejo{}, pollStore(pluginVersion(4, "git", "push", "")), eng, newRegistry(t), testConfig())

		_, err := svc.StartFromEvent(t.Context(), pluginEvent(`{}`))

		require.ErrorIs(t, err, errBoom)
	})

	t.Run("latest versions error returns error", func(t *testing.T) {
		store := &MockStore{
			LatestWorkflowVersionsFunc: func(context.Context) ([]process.WorkflowVersion, error) { return nil, errBoom },
		}
		svc := New(&forgejo.MockForgejo{}, store, &MockEngine{}, newRegistry(t), testConfig())

		_, err := svc.StartFromEvent(t.Context(), pluginEvent(`{}`))

		require.ErrorIs(t, err, errBoom)
	})

	t.Run("invalid payload returns error", func(t *testing.T) {
		svc := New(&forgejo.MockForgejo{}, &MockStore{}, &MockEngine{}, newRegistry(t), testConfig())

		_, err := svc.StartFromEvent(t.Context(), pluginEvent(`{bad`))

		require.Error(t, err)
	})
}
