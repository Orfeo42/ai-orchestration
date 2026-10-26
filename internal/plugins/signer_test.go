package plugins

import (
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cbQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Query()
}

func TestSignerCallback(t *testing.T) {
	deadline := time.Unix(1_000_000, 0)
	signer := NewSigner([]byte("secret"), "http://core/")
	cb := signer.Callback("job/1", deadline)

	t.Run("builds urls on the base with escaped job id", func(t *testing.T) {
		assert.Contains(t, cb.ResultURL, "http://core/api/v1/jobs/job%2F1/result?")
		assert.Contains(t, cb.ProgressURL, "http://core/api/v1/jobs/job%2F1/progress?")
	})

	t.Run("exp is deadline plus grace", func(t *testing.T) {
		want := strconv.FormatInt(deadline.Add(10*time.Minute+SignatureGrace).Unix(), 10)
		assert.Equal(t, want, cbQuery(t, cb.ResultURL).Get("exp"))
		assert.Equal(t, want, cbQuery(t, cb.ProgressURL).Get("exp"))
	})

	t.Run("kinds get different signatures", func(t *testing.T) {
		assert.NotEqual(t, cbQuery(t, cb.ResultURL).Get("sig"), cbQuery(t, cb.ProgressURL).Get("sig"))
	})
}

func TestSignerVerify(t *testing.T) {
	deadline := time.Unix(1_000_000, 0)
	signer := NewSigner([]byte("secret"), "http://core")
	cb := signer.Callback("job-1", deadline)
	now := deadline

	t.Run("round trip verifies", func(t *testing.T) {
		require.NoError(t, signer.Verify(CallbackResult, "job-1", cbQuery(t, cb.ResultURL), now))
		require.NoError(t, signer.Verify(CallbackProgress, "job-1", cbQuery(t, cb.ProgressURL), now))
	})

	t.Run("tampered sig is invalid", func(t *testing.T) {
		q := cbQuery(t, cb.ResultURL)
		q.Set("sig", "00"+q.Get("sig")[2:])
		require.ErrorIs(t, signer.Verify(CallbackResult, "job-1", q, now), ErrInvalidSignature)
	})

	t.Run("tampered exp is invalid", func(t *testing.T) {
		q := cbQuery(t, cb.ResultURL)
		exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
		require.NoError(t, err)
		q.Set("exp", strconv.FormatInt(exp+1, 10))
		require.ErrorIs(t, signer.Verify(CallbackResult, "job-1", q, now), ErrInvalidSignature)
	})

	t.Run("other job id is invalid", func(t *testing.T) {
		require.ErrorIs(t, signer.Verify(CallbackResult, "job-2", cbQuery(t, cb.ResultURL), now), ErrInvalidSignature)
	})

	t.Run("other kind is invalid", func(t *testing.T) {
		require.ErrorIs(t, signer.Verify(CallbackProgress, "job-1", cbQuery(t, cb.ResultURL), now), ErrInvalidSignature)
	})

	t.Run("other secret is invalid", func(t *testing.T) {
		other := NewSigner([]byte("other"), "http://core")
		require.ErrorIs(t, other.Verify(CallbackResult, "job-1", cbQuery(t, cb.ResultURL), now), ErrInvalidSignature)
	})

	t.Run("expired", func(t *testing.T) {
		late := deadline.Add(10*time.Minute + SignatureGrace + time.Second)
		require.ErrorIs(t, signer.Verify(CallbackResult, "job-1", cbQuery(t, cb.ResultURL), late), ErrExpiredSignature)
	})

	t.Run("missing params", func(t *testing.T) {
		require.ErrorIs(t, signer.Verify(CallbackResult, "job-1", url.Values{}, now), ErrInvalidSignature)
	})

	t.Run("unparsable exp", func(t *testing.T) {
		q := cbQuery(t, cb.ResultURL)
		q.Set("exp", "soon")
		require.ErrorIs(t, signer.Verify(CallbackResult, "job-1", q, now), ErrInvalidSignature)
	})

	t.Run("bad hex sig", func(t *testing.T) {
		q := cbQuery(t, cb.ResultURL)
		q.Set("sig", "zz")
		require.ErrorIs(t, signer.Verify(CallbackResult, "job-1", q, now), ErrInvalidSignature)
	})
}
