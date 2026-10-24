package pluginapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sampleInput struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestCompileSchema(t *testing.T) {
	t.Run("valid schema compiles", func(t *testing.T) {
		s, err := CompileSchema(json.RawMessage(`{"type":"object"}`))
		require.NoError(t, err)
		assert.NotNil(t, s)
	})

	t.Run("empty raw is rejected", func(t *testing.T) {
		_, err := CompileSchema(nil)
		require.ErrorIs(t, err, ErrInvalidSchema)
	})

	t.Run("whitespace raw is rejected", func(t *testing.T) {
		_, err := CompileSchema(json.RawMessage("  \n"))
		require.ErrorIs(t, err, ErrInvalidSchema)
	})

	t.Run("invalid json is rejected", func(t *testing.T) {
		_, err := CompileSchema(json.RawMessage(`{`))
		require.ErrorIs(t, err, ErrInvalidSchema)
	})

	t.Run("invalid schema is rejected", func(t *testing.T) {
		_, err := CompileSchema(json.RawMessage(`{"type": 5}`))
		require.ErrorIs(t, err, ErrInvalidSchema)
	})
}

func TestSchemaValidate(t *testing.T) {
	objectSchema, err := CompileSchema(json.RawMessage(`{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}}`))
	require.NoError(t, err)

	t.Run("matching document passes", func(t *testing.T) {
		require.NoError(t, objectSchema.Validate(json.RawMessage(`{"a":"x"}`)))
	})

	t.Run("violating document fails", func(t *testing.T) {
		err := objectSchema.Validate(json.RawMessage(`{"a":1}`))
		require.ErrorIs(t, err, ErrSchemaViolation)
	})

	t.Run("invalid json fails", func(t *testing.T) {
		err := objectSchema.Validate(json.RawMessage(`{`))
		require.ErrorIs(t, err, ErrSchemaViolation)
	})

	t.Run("empty document is null and rejected by object schema", func(t *testing.T) {
		err := objectSchema.Validate(nil)
		require.ErrorIs(t, err, ErrSchemaViolation)
	})

	t.Run("empty document passes a null schema", func(t *testing.T) {
		nullSchema, err := CompileSchema(json.RawMessage(`{"type":"null"}`))
		require.NoError(t, err)
		require.NoError(t, nullSchema.Validate(nil))
	})
}

func TestSchemaFor(t *testing.T) {
	raw, err := SchemaFor[sampleInput]()
	require.NoError(t, err)

	s, err := CompileSchema(raw)
	require.NoError(t, err)

	t.Run("accepts matching document", func(t *testing.T) {
		require.NoError(t, s.Validate(json.RawMessage(`{"name":"a","count":2}`)))
	})

	t.Run("rejects wrong typed field", func(t *testing.T) {
		err := s.Validate(json.RawMessage(`{"name":"a","count":"two"}`))
		require.ErrorIs(t, err, ErrSchemaViolation)
	})

	t.Run("unsupported type is wrapped", func(t *testing.T) {
		_, err := SchemaFor[chan int]()
		require.ErrorIs(t, err, ErrInvalidSchema)
	})
}
