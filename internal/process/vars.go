package process

import (
	"encoding/json"
	"fmt"
	"maps"
)

type Vars map[string]json.RawMessage

type Key[T any] string

func Get[T any](v Vars, k Key[T]) (T, bool, error) {
	var out T
	raw, ok := v[string(k)]
	if !ok {
		return out, false, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, false, fmt.Errorf("decode var %s: %w", k, err)
	}
	return out, true, nil
}

func Set[T any](v Vars, k Key[T], value T) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode var %s: %w", k, err)
	}
	v[string(k)] = raw
	return nil
}

func (v Vars) Merge(other Vars) Vars {
	out := make(Vars, len(v)+len(other))
	maps.Copy(out, v)
	maps.Copy(out, other)
	return out
}
