package gemini

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

var (
	errQuota = errors.New("RESOURCE_EXHAUSTED: quota")
	errOther = errors.New("boom")
)

func TestFallbackReviewer_Review(t *testing.T) {
	review := func(res models.ReviewResult, err error) *MockReviewer {
		return &MockReviewer{ReviewFunc: func(context.Context, models.ReviewJob, events.Sink) (models.ReviewResult, error) {
			return res, err
		}}
	}

	t.Run("primary ok", func(t *testing.T) {
		secondary := review(models.ReviewResult{}, nil)
		r := FallbackReviewer{Primary: review(models.ReviewResult{Summary: "p"}, nil), Secondary: secondary}
		got, err := r.Review(context.Background(), models.ReviewJob{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "p", got.Summary)
		assert.Empty(t, secondary.ReviewCalls())
	})

	t.Run("quota error falls back to secondary", func(t *testing.T) {
		r := FallbackReviewer{Primary: review(models.ReviewResult{}, errQuota), Secondary: review(models.ReviewResult{Summary: "s"}, nil)}
		got, err := r.Review(context.Background(), models.ReviewJob{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "s", got.Summary)
	})

	t.Run("non-quota error returned without fallback", func(t *testing.T) {
		secondary := review(models.ReviewResult{}, nil)
		r := FallbackReviewer{Primary: review(models.ReviewResult{}, errOther), Secondary: secondary}
		_, err := r.Review(context.Background(), models.ReviewJob{}, nil)
		require.ErrorIs(t, err, errOther)
		assert.Empty(t, secondary.ReviewCalls())
	})
}

func TestFallbackDocsWriter_UpdateReadme(t *testing.T) {
	docs := func(updated bool, err error) *MockDocsWriter {
		return &MockDocsWriter{UpdateReadmeFunc: func(context.Context, models.ReviewJob, string, models.ReviewResult, events.Sink) (bool, error) {
			return updated, err
		}}
	}

	t.Run("primary ok", func(t *testing.T) {
		secondary := docs(false, nil)
		d := FallbackDocsWriter{Primary: docs(true, nil), Secondary: secondary}
		updated, err := d.UpdateReadme(context.Background(), models.ReviewJob{}, "p", models.ReviewResult{}, nil)
		require.NoError(t, err)
		assert.True(t, updated)
		assert.Empty(t, secondary.UpdateReadmeCalls())
	})

	t.Run("quota error falls back to secondary", func(t *testing.T) {
		d := FallbackDocsWriter{Primary: docs(false, errQuota), Secondary: docs(true, nil)}
		updated, err := d.UpdateReadme(context.Background(), models.ReviewJob{}, "p", models.ReviewResult{}, nil)
		require.NoError(t, err)
		assert.True(t, updated)
	})

	t.Run("non-quota error returned without fallback", func(t *testing.T) {
		secondary := docs(true, nil)
		d := FallbackDocsWriter{Primary: docs(false, errOther), Secondary: secondary}
		_, err := d.UpdateReadme(context.Background(), models.ReviewJob{}, "p", models.ReviewResult{}, nil)
		require.ErrorIs(t, err, errOther)
		assert.Empty(t, secondary.UpdateReadmeCalls())
	})
}
