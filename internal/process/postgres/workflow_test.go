package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/process"
)

func TestUpsertWorkflowVersion(t *testing.T) {
	t.Run("same hash returns same id", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		v := process.WorkflowVersion{Name: "other", Hash: "h", Definition: []byte(`{"a":1}`), SourceYAML: "y"}

		first, err := e.store.UpsertWorkflowVersion(ctx, v)
		require.NoError(t, err)
		second, err := e.store.UpsertWorkflowVersion(ctx, v)

		require.NoError(t, err)
		assert.Equal(t, first.ID, second.ID)
		assert.JSONEq(t, `{"a":1}`, string(second.Definition))
	})

	t.Run("new hash returns new id", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		first, err := e.store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{Name: "other", Hash: "h1", Definition: []byte(`{}`), SourceYAML: "y"})
		require.NoError(t, err)

		second, err := e.store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{Name: "other", Hash: "h2", Definition: []byte(`{}`), SourceYAML: "y"})

		require.NoError(t, err)
		assert.NotEqual(t, first.ID, second.ID)
	})
}

func TestGetWorkflowVersion(t *testing.T) {
	t.Run("existing id is returned", func(t *testing.T) {
		e := newEnv(t)

		got, err := e.store.GetWorkflowVersion(context.Background(), e.versionID)

		require.NoError(t, err)
		assert.Equal(t, "wf", got.Name)
		assert.Equal(t, "name: wf", got.SourceYAML)
	})

	t.Run("unknown id returns not found", func(t *testing.T) {
		e := newEnv(t)

		_, err := e.store.GetWorkflowVersion(context.Background(), 999999)

		require.ErrorIs(t, err, process.ErrNotFound)
	})
}

func TestLatestWorkflowVersions(t *testing.T) {
	t.Run("returns current version per name ordered by name", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		newest, err := e.store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{Name: "wf", Hash: "h2", Definition: []byte(`{}`), SourceYAML: "y"})
		require.NoError(t, err)
		other, err := e.store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{Name: "other", Hash: "h1", Definition: []byte(`{}`), SourceYAML: "y"})
		require.NoError(t, err)
		require.NoError(t, e.store.SetCurrentWorkflow(ctx, "wf", newest.ID))
		require.NoError(t, e.store.SetCurrentWorkflow(ctx, "other", other.ID))

		got, err := e.store.LatestWorkflowVersions(ctx)

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, other.ID, got[0].ID)
		assert.Equal(t, newest.ID, got[1].ID)
	})
}

func TestSetCurrentWorkflow(t *testing.T) {
	t.Run("switches latest to the given version", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		require.NoError(t, e.store.SetCurrentWorkflow(ctx, "wf", e.versionID))
		newer, err := e.store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{Name: "wf", Hash: "h2", Definition: []byte(`{}`), SourceYAML: "y"})
		require.NoError(t, err)

		require.NoError(t, e.store.SetCurrentWorkflow(ctx, "wf", newer.ID))
		got, err := e.store.LatestWorkflowVersions(ctx)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, newer.ID, got[0].ID)

		require.NoError(t, e.store.SetCurrentWorkflow(ctx, "wf", e.versionID))
		got, err = e.store.LatestWorkflowVersions(ctx)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, e.versionID, got[0].ID)
	})
}

func TestSeedWorkflow(t *testing.T) {
	t.Run("inserts once then returns false", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		newer, err := e.store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{Name: "wf", Hash: "h2", Definition: []byte(`{}`), SourceYAML: "y"})
		require.NoError(t, err)

		inserted, err := e.store.SeedWorkflow(ctx, "wf", e.versionID)
		require.NoError(t, err)
		assert.True(t, inserted)
		inserted, err = e.store.SeedWorkflow(ctx, "wf", newer.ID)
		require.NoError(t, err)
		assert.False(t, inserted)

		got, err := e.store.LatestWorkflowVersions(ctx)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, e.versionID, got[0].ID)
	})
}

func TestDeleteWorkflow(t *testing.T) {
	t.Run("existing workflow is removed from latest and versions are kept", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		require.NoError(t, e.store.SetCurrentWorkflow(ctx, "wf", e.versionID))

		deleted, err := e.store.DeleteWorkflow(ctx, "wf")

		require.NoError(t, err)
		assert.True(t, deleted)
		latest, err := e.store.LatestWorkflowVersions(ctx)
		require.NoError(t, err)
		assert.Empty(t, latest)
		versions, err := e.store.ListWorkflowVersions(ctx, "wf")
		require.NoError(t, err)
		assert.Len(t, versions, 1)
	})

	t.Run("unknown workflow returns false", func(t *testing.T) {
		e := newEnv(t)

		deleted, err := e.store.DeleteWorkflow(context.Background(), "nope")

		require.NoError(t, err)
		assert.False(t, deleted)
	})
}

func TestListWorkflowVersions(t *testing.T) {
	t.Run("returns versions of a name newest first", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		newest, err := e.store.UpsertWorkflowVersion(ctx, process.WorkflowVersion{Name: "wf", Hash: "h2", Definition: []byte(`{}`), SourceYAML: "y"})
		require.NoError(t, err)

		got, err := e.store.ListWorkflowVersions(ctx, "wf")

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, newest.ID, got[0].ID)
		assert.Equal(t, e.versionID, got[1].ID)
	})

	t.Run("unknown name returns empty", func(t *testing.T) {
		e := newEnv(t)

		got, err := e.store.ListWorkflowVersions(context.Background(), "nope")

		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
