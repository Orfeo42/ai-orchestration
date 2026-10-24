package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQueues(t *testing.T) {
	t.Run("valid list", func(t *testing.T) {
		got, err := parseQueues("claude, default")
		require.NoError(t, err)
		assert.Equal(t, []string{"claude", "default"}, got)
	})

	t.Run("single queue", func(t *testing.T) {
		got, err := parseQueues("default")
		require.NoError(t, err)
		assert.Equal(t, []string{"default"}, got)
	})

	t.Run("empty", func(t *testing.T) {
		_, err := parseQueues(" , ")
		require.ErrorIs(t, err, errNoQueues)
	})

	t.Run("unknown", func(t *testing.T) {
		_, err := parseQueues("claude,bogus")
		require.ErrorContains(t, err, string(errUnknownQueue))
	})

	t.Run("duplicate", func(t *testing.T) {
		_, err := parseQueues("default,default")
		require.ErrorContains(t, err, string(errDuplicateQueue))
	})
}
