package impl

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/models"
)

var errPrimary = errors.New("primary down")

func writer(desc string, err error) *gemini.MockPRDescWriter {
	return &gemini.MockPRDescWriter{GenerateFunc: func(context.Context, models.ForgejoIssue, string, string, events.Sink) (string, error) {
		return desc, err
	}}
}

func TestFallbackPRWriter_Generate(t *testing.T) {
	t.Run("primary ok", func(t *testing.T) {
		fallback := writer("f", nil)
		w := &FallbackPRWriter{Primary: writer("p", nil), Fallback: fallback}
		got, err := w.Generate(context.Background(), models.ForgejoIssue{}, "path", "diff", nil)
		require.NoError(t, err)
		assert.Equal(t, "p", got)
		assert.Empty(t, fallback.GenerateCalls())
	})

	t.Run("primary error uses fallback", func(t *testing.T) {
		w := &FallbackPRWriter{Primary: writer("", errPrimary), Fallback: writer("f", nil)}
		got, err := w.Generate(context.Background(), models.ForgejoIssue{}, "path", "diff", nil)
		require.NoError(t, err)
		assert.Equal(t, "f", got)
	})

	t.Run("primary error without fallback is returned", func(t *testing.T) {
		w := &FallbackPRWriter{Primary: writer("", errPrimary)}
		_, err := w.Generate(context.Background(), models.ForgejoIssue{}, "path", "diff", nil)
		require.ErrorIs(t, err, errPrimary)
	})
}
