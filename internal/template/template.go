package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type templateError string

func (e templateError) Error() string { return string(e) }

const (
	ErrSyntax     templateError = "invalid expression"
	ErrUnresolved templateError = "expression path not found"
	ErrNotScalar  templateError = "interpolated value is not a scalar"
	ErrInvalidDoc templateError = "invalid json document"
)

const (
	open   = "${"
	escape = "$${"
)

var segmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$|^[0-9]+$`)

type part struct {
	literal string
	path    []string
}

func HasExpressions(doc json.RawMessage) bool {
	v, err := decode(doc)
	if err != nil {
		return false
	}
	return hasExpr(v)
}

func hasExpr(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.Contains(strings.ReplaceAll(t, escape, ""), open)
	case map[string]any:
		for _, item := range t {
			if hasExpr(item) {
				return true
			}
		}
	case []any:
		for _, item := range t {
			if hasExpr(item) {
				return true
			}
		}
	}
	return false
}

func Resolve(doc json.RawMessage, scope json.RawMessage) (json.RawMessage, error) {
	v, err := decode(doc)
	if err != nil {
		return nil, err
	}
	s, err := decode(scope)
	if err != nil {
		return nil, err
	}
	out, err := walk(v, s)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDoc, err)
	}
	return raw, nil
}

func decode(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDoc, err)
	}
	return v, nil
}

func walk(v, scope any) (any, error) {
	switch t := v.(type) {
	case string:
		return resolveString(t, scope)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			r, err := walk(item, scope)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			r, err := walk(item, scope)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = r
		}
		return out, nil
	}
	return v, nil
}

func resolveString(s string, scope any) (any, error) {
	parts, err := parse(s)
	if err != nil {
		return nil, err
	}
	if len(parts) == 1 && parts[0].path != nil {
		return lookup(scope, parts[0].path)
	}
	var b strings.Builder
	for _, p := range parts {
		if p.path == nil {
			b.WriteString(p.literal)
			continue
		}
		v, err := lookup(scope, p.path)
		if err != nil {
			return nil, err
		}
		text, err := scalarText(v, p.path)
		if err != nil {
			return nil, err
		}
		b.WriteString(text)
	}
	return b.String(), nil
}

func parse(s string) ([]part, error) {
	var parts []part
	var lit strings.Builder
	for len(s) > 0 {
		if strings.HasPrefix(s, escape) {
			lit.WriteString(open)
			s = s[len(escape):]
			continue
		}
		if !strings.HasPrefix(s, open) {
			lit.WriteByte(s[0])
			s = s[1:]
			continue
		}
		end := strings.IndexByte(s, '}')
		if end < 0 {
			return nil, fmt.Errorf("%w: unterminated %q", ErrSyntax, s)
		}
		path, err := parsePath(s[len(open):end])
		if err != nil {
			return nil, err
		}
		if lit.Len() > 0 {
			parts = append(parts, part{literal: lit.String()})
			lit.Reset()
		}
		parts = append(parts, part{path: path})
		s = s[end+1:]
	}
	if lit.Len() > 0 || len(parts) == 0 {
		parts = append(parts, part{literal: lit.String()})
	}
	return parts, nil
}

func parsePath(expr string) ([]string, error) {
	segments := strings.Split(strings.TrimSpace(expr), ".")
	for _, seg := range segments {
		if !segmentPattern.MatchString(seg) {
			return nil, fmt.Errorf("%w: %q", ErrSyntax, expr)
		}
	}
	return segments, nil
}

func lookup(scope any, path []string) (any, error) {
	current := scope
	for _, seg := range path {
		next, ok := child(current, seg)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnresolved, strings.Join(path, "."))
		}
		current = next
	}
	return current, nil
}

func child(v any, seg string) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		item, ok := t[seg]
		return item, ok
	case []any:
		i, err := strconv.Atoi(seg)
		if err != nil || i < 0 || i >= len(t) {
			return nil, false
		}
		return t[i], true
	}
	return nil, false
}

func scalarText(v any, path []string) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case json.Number:
		return t.String(), nil
	case bool:
		return strconv.FormatBool(t), nil
	}
	return "", fmt.Errorf("%w: %s", ErrNotScalar, strings.Join(path, "."))
}
