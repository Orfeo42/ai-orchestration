# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

Three-binary Go system drives AI dev loop:

- **`cmd/orchestrator`** polls Forgejo, assesses issues w/ Claude, impls via Claude Code CLI (`exec.CommandContext("claude", ...)`), runs tests, opens PRs, queues review jobs in Redis.
- **`cmd/reviewer`** consumes `queue:review`, reviews PR diffs w/ Gemini 2.0 Flash, updates READMEs, posts review comments.
- **`cmd/webui`** embedded React SPA + REST/WebSocket server on `127.0.0.1:8080`. Shows live stream-json events, run history, aggregate token usage. Human can stop or inject instructions into running Claude session.

All three share `internal/` packages, run as separate docker-compose services. Redis = single source of truth for run state, events, control signals, job queues.

## Build / test / run

Use `justfile` — every workflow has recipe (`just --list`).

- `just up` — `docker compose up --build -d` (builds frontend inside `web/Dockerfile`, no local npm needed)
- `just run-{orch,reviewer,webui}` — run single binary locally (needs local Redis; sets `REDIS_URL=redis://127.0.0.1:6379`)
- `just web-dev` — Vite dev server on `:5173`, proxies `/api` + `/ws` to `:8080`
- `just go-test` — `go test -short ./...` (unit tests, miniredis-backed, no docker)
- `just generate` — regen moq mocks after changing any `Agent`/`Channel`/`Sink`/`Store`/`Forgejo`/`gemini.*`/`Queue`/`Git` interface
- `just check` — vet + test

Single test: `go test -run TestParseClaudeLine/assistant_tool_use ./internal/events/...`

## Architecture that requires multiple files to understand

### Event flow (the heart of the system)

Claude's `--output-format stream-json` lines = source of truth for AI activity. Data path:

```text
claude subprocess stdout (NDJSON)
  → internal/agent/claude.go: runClaudeStream scans line-by-line
  → internal/events/parse.go: ParseClaudeLine(line) → []events.Event
  → events.Sink.Emit (injected per invocation)
  → runstore.AppendSink (XADD to run:{id}:events, INCR seq counter)
  → webui WebSocket handler (XRANGE replay → XREAD BLOCK tail)
  → React EventLog component
```

**Change event capture/display → start at `internal/events/parse.go`.** Add new `Kind` constant, teach parser to emit it. Rest of stack (store → webui REST → WS → React) flows events through via generic `Event` struct.

### Token accounting

Claude emits `type=result` event w/ `usage` field: `input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`. Gemini returns `resp.UsageMetadata`. Both converge in `events.TokenUsage`, accumulates per run (`runstore.Update` callbacks in `pipeline.go`), flushes to `tokens:daily:{YYYY-MM-DD}` HASH via `HINCRBY` on `store.Finalize`.

### Cancel / inject protocol

UI click → REST `POST /api/runs/{id}/cancel|inject` → Redis `PUBLISH ctrl:{runID}` → worker's `control.Subscribe` goroutine → calls `runHandle.cancel` `CancelFunc` → `exec.CommandContext` SIGKILLs Claude. Injected notes drained on next loop iteration of `processIssue`, prepended to next prompt. `--resume {sessionID}` keeps Claude's conversation state.

**Session pinning load-bearing**: every `AssessIssue` + `ImplementIssue` captures `session_id` from stream-json `system`/`init` event, threads into next call via `--resume`. Don't replace w/ `--continue` — serial issues sharing project dir would clobber each other.

### Store / control interfaces

`internal/runstore/iface.go` + `internal/control/iface.go` = boundary between domain + Redis. Both have moq mocks (`store_mock.go`, `channel_mock.go`). Tests use `alicebob/miniredis/v2` not real Redis — intentional (fast, no docker).

`runstore.AppendSink(runID)` returns `events.Sink` — Claude runner emits events w/o knowing about store. Sinks compose via `events.Tee(...)`.

### `internal/runtime`

Thin shared main.go boilerplate for all three binaries: `InitLogger`, `ShutdownContext` (SIGINT/SIGTERM), `NewStore/NewControl/NewQueue`, `MustEnv/EnvOr`, `ConfigureGit`, `CloseAll`, `Die`. Use in `cmd/*/main.go` — don't duplicate.

## Project-specific gotchas

- **`cmd/webui/dist/index.html` must exist** even w/o frontend build, because `//go:embed all:dist` in `cmd/webui/assets.go` fails compilation if dir empty. `just clean` preserves it.
- **Vite outputs to `../cmd/webui/dist`** (configured in `web/vite.config.ts`), _not_ `web/dist`. So `go:embed` picks it up from Go binary's own package.
- **`logRequest` middleware wraps `http.ResponseWriter` w/ `statusRecorder`**, must impl `http.Hijacker` for WebSocket upgrades. New wrapper → preserve Hijacker pass-through.
- **`coder/websocket` not `gorilla/websocket`** — small, context-aware, maintained.
- **Claude Code CLI auth** subscription-based via `config/claude/.credentials.json` baked into orchestrator image. No `ANTHROPIC_API_KEY` env var.
- **LSP diagnostics go stale** during big refactors (errors about old interface signature after regen mocks). Trust `go build ./...` + `go test ./...`, not IDE squiggles.

## Code style (project-specific additions to user's global CLAUDE.md)

- Agent/Gemini methods accept `events.Sink` as last param — every AI call must pipe events through sink for UI display.
- Claude methods return `agent.AssessResult` or `agent.ImplementResult` (never bare tuples) → stays within 2-return-values rule w/ session_id + usage baked into result struct.
- Mocks generated w/ `moq`, not handwritten. After interface change: `just generate`.
- Tests avoid loops over slice-of-struct table fixtures — use explicit `t.Run` subtests (pattern in `internal/events/parse_test.go`, `cmd/webui/handlers_test.go`).

## Where to start when asked to…

- **Add new Claude event kind**: `internal/events/{event.go,parse.go,parse_test.go}` → React `web/src/components/EventLog.tsx`.
- **Add new REST endpoint**: `cmd/webui/handlers.go` + route in `cmd/webui/server.go` + `cmd/webui/handlers_test.go`. Client in `web/src/api.ts`.
- **Add new run phase**: `internal/runstore/run.go` const block → emit via `handle.setPhase` in `cmd/orchestrator/pipeline.go` or `cmd/reviewer/pipeline.go` → React styling in `web/src/styles.css` (`.phase-<name>` class).
- **Change Gemini prompts or token parsing**: `internal/gemini/*.go`. Usage emission via `emitUsage` helper in `gemini.go`.
- **Change Claude invocation args or timeout behavior**: `internal/agent/claude.go` — `runClaudeStream` = single entry point shared by `AssessIssue`, `ImplementIssue`, `prdesc.Generate`.
