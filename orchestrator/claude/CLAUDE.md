# Environment

- OS: Debian (container)
- Shell: bash
- Go project layout: standard (`cmd/`, `internal/`, `pkg/`)
- Python package manager: uv
- No Docker daemon. Integration tests use testcontainers → always run Go tests with `-short` to skip them.

## Code style

### General

- Short functions. One responsibility.
- Early returns. Flat control flow.
- Check/propagate all errors.
- Add logs for new code.
- No suppressed warnings.
- No comments. Remove useless comments in touched files.

### Go Style

- No `any`/`interface{}` unless no alternative.
- Check all errors. No `_` assignment.
- `fmt.Errorf` with `%w` for wrapping.
- Standard naming: unexported default, short receivers, no stutter (e.g., `user.New`, not `user.NewUser`).

### Python Style

- Type hints on all signatures and non-obvious vars.
- No bare `except:`. Catch specific exceptions.
- `pathlib.Path` over `os.path`.
- No weak typing: never `Any`, bare `dict`, bare `list`, bare `tuple`, or `object`. Always parameterize generics. Use `TypedDict` or Pydantic for structured data.

## Testing

### Go Tests

- Use `testing` package.
- `t.Run` subtests:

```go
func TestParseID(t *testing.T) {
    t.Run("valid numeric ID", func(t *testing.T) {
        got, err := ParseID("42")
        require.NoError(t, err)
        assert.Equal(t, 42, got)
    })

    t.Run("empty string returns error", func(t *testing.T) {
        _, err := ParseID("")
        require.Error(t, err)
    })
}
```

- No struct slice with input/expected fields.
- One test func per func under test.
- Separate explicit `t.Run` per case. No loops over slices.
- Mock all mockable external deps (interfaces).

### Python Tests

- Use `pytest`.
- Mock with `unittest.mock` or `pytest-mock`.

## Interaction rules

- Read official docs before answering questions about libraries, tools, APIs

## Communication

/caveman ultra
