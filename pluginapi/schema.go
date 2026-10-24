package pluginapi

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

type Schema struct {
	resolved *jsonschema.Resolved
}

func CompileSchema(raw json.RawMessage) (*Schema, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: empty schema", ErrInvalidSchema)
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}
	resolved, err := parsed.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}
	return &Schema{resolved: resolved}, nil
}

func (s *Schema) Validate(doc json.RawMessage) error {
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = json.RawMessage("null")
	}
	var instance any
	if err := json.Unmarshal(doc, &instance); err != nil {
		return fmt.Errorf("%w: %w", ErrSchemaViolation, err)
	}
	if err := s.resolved.Validate(instance); err != nil {
		return fmt.Errorf("%w: %w", ErrSchemaViolation, err)
	}
	return nil
}

func SchemaFor[T any]() (json.RawMessage, error) {
	inferred, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}
	raw, err := json.Marshal(inferred)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}
	return raw, nil
}
