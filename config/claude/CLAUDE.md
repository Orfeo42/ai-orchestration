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
- Error shoudld be constants
- Standard naming: unexported default, short receivers, no stutter (e.g., `user.New`, not `user.NewUser`).
- Structured Logging: Always use `log/slog`.
  - Initialize a global logger (JSON is best).
  - Use strongly-typed attributes (e.g., `slog.String`, `slog.Int`) or key-value pairs.
  - Prefer `Logger.InfoContext` or `Logger.ErrorContext` to ensure trace propagation if context contains trace IDs.
  - Never use `fmt.Printf` or the standard `log` package for application logic.
- func with max 2 return values (1 value, 1 error)

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
- run test with this command: `go test -short ./... | grep -vE "^(ok|[[:space:]]*\?|.*Redismq|PASS)"`
- test all the new code
- use moc to generate mock:

```go
//go:generate moq -out service_mock.go . IService:MockService
type IService interface {
    GenerateStatement(ctx context.Context, userId uint64, year *int) ([]byte, error)
}
```

### Python Tests

- Use `pytest`.
- Mock with `unittest.mock` or `pytest-mock`.
- test all the new code

## Interaction rules

- Read official documentation before answering questions about libraries, tools, or APIs.
