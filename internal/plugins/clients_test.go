package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

func TestClientsFor(t *testing.T) {
	t.Run("returns a client when the token is set", func(t *testing.T) {
		_, clients, _ := catFixture(t, catTokens)
		pc, err := clients.For("alpha", "http://alpha")
		require.NoError(t, err)
		assert.NotNil(t, pc)
	})

	t.Run("missing token returns ErrNoToken", func(t *testing.T) {
		_, clients, _ := catFixture(t, catNoTokens)
		_, err := clients.For("alpha", "http://alpha")
		require.ErrorIs(t, err, ErrNoToken)
	})
}

func TestClientsAuthenticate(t *testing.T) {
	_, clients, _ := catFixture(t, catTokens)

	t.Run("correct token", func(t *testing.T) {
		require.NoError(t, clients.Authenticate("alpha", catToken))
	})

	t.Run("wrong token", func(t *testing.T) {
		require.ErrorIs(t, clients.Authenticate("alpha", "wrong"), ErrUnauthorized)
	})

	t.Run("missing token configuration", func(t *testing.T) {
		_, noTokens, _ := catFixture(t, catNoTokens)
		err := noTokens.Authenticate("alpha", catToken)
		require.ErrorIs(t, err, ErrUnauthorized)
		require.ErrorIs(t, err, ErrNoToken)
	})
}

func TestClientsCancelJob(t *testing.T) {
	t.Run("cancels a running job on the plugin", func(t *testing.T) {
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		ts, m := catServe(t, "alpha", catBlockingAction("act", release))
		catalog, clients, _ := catFixture(t, catTokens, catRecord("alpha", ts.URL, m))

		pc, err := clients.For("alpha", ts.URL)
		require.NoError(t, err)
		deadline := time.Now().UTC().Add(time.Minute)
		_, err = pc.StartJob(context.Background(), "act", pluginapi.JobRequest{
			JobID:            "run-1",
			ProcessID:        "p1",
			Input:            []byte(`{"n":1}`),
			Callback:         catalog.signer.Callback("run-1", deadline),
			Deadline:         deadline,
			HeartbeatSeconds: 30,
		})
		require.NoError(t, err)

		require.NoError(t, clients.CancelJob(context.Background(), "alpha", "run-1"))
	})

	t.Run("unknown plugin returns ErrNotFound", func(t *testing.T) {
		_, clients, _ := catFixture(t, catTokens)
		require.ErrorIs(t, clients.CancelJob(context.Background(), "alpha", "run-1"), ErrNotFound)
	})

	t.Run("missing token returns ErrNoToken", func(t *testing.T) {
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)})
		_, clients, _ := catFixture(t, catNoTokens, catRecord("alpha", "http://alpha", m))
		require.ErrorIs(t, clients.CancelJob(context.Background(), "alpha", "run-1"), ErrNoToken)
	})

	t.Run("unknown job returns plugin error", func(t *testing.T) {
		ts, m := catServe(t, "alpha", catSyncAction("act", pluginapi.Output{}, nil))
		_, clients, _ := catFixture(t, catTokens, catRecord("alpha", ts.URL, m))
		require.ErrorIs(t, clients.CancelJob(context.Background(), "alpha", "nope"), pluginapi.ErrJobNotFound)
	})
}
