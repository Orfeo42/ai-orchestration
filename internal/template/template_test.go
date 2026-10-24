package template

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const scope = `{"vars":{"assess":{"plan":"do it","n":3,"ok":true,"list":["a","b"],"obj":{"k":"v"}}},"subject":{"key":"o/r#1"}}`

func TestHasExpressions(t *testing.T) {
	t.Run("nested expression", func(t *testing.T) {
		assert.True(t, HasExpressions(json.RawMessage(`{"a":[{"b":"x ${vars.y}"}]}`)))
	})

	t.Run("plain document", func(t *testing.T) {
		assert.False(t, HasExpressions(json.RawMessage(`{"a":"$ {x}","n":1}`)))
	})

	t.Run("escaped expression only", func(t *testing.T) {
		assert.False(t, HasExpressions(json.RawMessage(`{"a":"$${x}"}`)))
	})

	t.Run("invalid json", func(t *testing.T) {
		assert.False(t, HasExpressions(json.RawMessage(`{`)))
	})
}

func TestResolve(t *testing.T) {
	t.Run("whole string keeps the value type", func(t *testing.T) {
		got, err := Resolve(json.RawMessage(`{"plan":"${vars.assess.obj}","n":"${vars.assess.n}","list":"${vars.assess.list}"}`), json.RawMessage(scope))
		require.NoError(t, err)
		assert.JSONEq(t, `{"plan":{"k":"v"},"n":3,"list":["a","b"]}`, string(got))
	})

	t.Run("interpolates scalars into a string", func(t *testing.T) {
		got, err := Resolve(json.RawMessage(`{"t":"${subject.key}: ${vars.assess.plan} (${vars.assess.n}, ${vars.assess.ok})"}`), json.RawMessage(scope))
		require.NoError(t, err)
		assert.JSONEq(t, `{"t":"o/r#1: do it (3, true)"}`, string(got))
	})

	t.Run("array index", func(t *testing.T) {
		got, err := Resolve(json.RawMessage(`["${vars.assess.list.1}"]`), json.RawMessage(scope))
		require.NoError(t, err)
		assert.JSONEq(t, `["b"]`, string(got))
	})

	t.Run("escape yields a literal", func(t *testing.T) {
		got, err := Resolve(json.RawMessage(`{"t":"$${vars.x} and ${subject.key}"}`), json.RawMessage(scope))
		require.NoError(t, err)
		assert.JSONEq(t, `{"t":"${vars.x} and o/r#1"}`, string(got))
	})

	t.Run("non string values untouched", func(t *testing.T) {
		got, err := Resolve(json.RawMessage(`{"n":12345678901234567890,"b":false,"z":null}`), json.RawMessage(scope))
		require.NoError(t, err)
		assert.JSONEq(t, `{"n":12345678901234567890,"b":false,"z":null}`, string(got))
	})

	t.Run("empty document", func(t *testing.T) {
		got, err := Resolve(nil, json.RawMessage(scope))
		require.NoError(t, err)
		assert.JSONEq(t, `{}`, string(got))
	})

	t.Run("missing path", func(t *testing.T) {
		_, err := Resolve(json.RawMessage(`{"a":"${vars.nope}"}`), json.RawMessage(scope))
		require.ErrorIs(t, err, ErrUnresolved)
	})

	t.Run("object interpolated into text", func(t *testing.T) {
		_, err := Resolve(json.RawMessage(`{"a":"x ${vars.assess.obj}"}`), json.RawMessage(scope))
		require.ErrorIs(t, err, ErrNotScalar)
	})

	t.Run("unterminated expression", func(t *testing.T) {
		_, err := Resolve(json.RawMessage(`{"a":"${vars.x"}`), json.RawMessage(scope))
		require.ErrorIs(t, err, ErrSyntax)
	})

	t.Run("bad path segment", func(t *testing.T) {
		_, err := Resolve(json.RawMessage(`{"a":"${vars..x}"}`), json.RawMessage(scope))
		require.ErrorIs(t, err, ErrSyntax)
	})

	t.Run("invalid document", func(t *testing.T) {
		_, err := Resolve(json.RawMessage(`{`), json.RawMessage(scope))
		require.ErrorIs(t, err, ErrInvalidDoc)
	})
}
