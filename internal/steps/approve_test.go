package steps

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/process"
)

func TestApproveStep(t *testing.T) {
	t.Run("waits for signal with no outcome", func(t *testing.T) {
		res, err := approveStep{}.Run(context.Background(), newScope(t, process.Vars{}))
		require.NoError(t, err)
		assert.Empty(t, res.Outcome)
		require.NotNil(t, res.Wait)
		assert.Equal(t, process.WaitSignal, res.Wait.Kind)
		assert.Empty(t, res.Wait.ResumeOutcome)
	})
}
