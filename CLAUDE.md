# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

Producer/consumer Go system split into three queue stages + a webui, glued by a generic `Processor[T any]` pipeline engine.

- **`cmd/producer`** polls Forgejo issues, pushes `models.AssessJob` → `queue:assess`.
- **`cmd/consumer`** single binary, dispatches on `--pipeline` flag:
  - `--pipeline=assess` pops `queue:assess`, runs assess pipeline (hydrate issue, fetch comments, Claude clarity check, enqueue impl). On unclear → posts clarification comment + adds `needs-info` label.
  - `--pipeline=impl` pops `queue:impl`, runs impl pipeline (checkout branch, retry loop of `ImplementAttempt → CheckHasChanges → RunTests`, open PR, enqueue review).
  - `--pipeline=review` pops `queue:review`, runs review pipeline (Gemini review, checkout PR branch, update README, post review).
- **`cmd/webui`** embedded React SPA + REST/WebSocket server on `127.0.0.1:8080`. Shows live stream-json events, run history, aggregate token usage. Human can stop or inject instructions into a running Claude session.

Each consumer type scales independently (`docker compose up --scale impl-consumer=3`). Redis = single source of truth for run state, events, control signals, queues. All three share `internal/` packages.

## Build / test / run

Use `justfile` — every workflow has a recipe (`just --list`).

- `just build-up` — `docker compose up --build -d` (brings up redis, producer, assess/impl/review consumers, webui).
- `just run-{producer,assess-consumer,impl-consumer,review-consumer,webui}` — run one binary locally (needs local Redis; sets `REDIS_URL=redis://127.0.0.1:6379`).
- `just web-dev` — Vite dev server on `:5173`, proxies `/api` + `/ws` to `:8080`.
- `just go-test` — `go test -short ./...` (miniredis-backed, no docker).
- `just generate` — regen moq mocks after interface edits.
- `just check` — vet + test.
- `just scale-impl n=3` — scale impl-consumer replicas.

Single test: `go test -run TestParseClaudeLine/assistant_tool_use ./internal/events/...`.

## Architecture that requires multiple files to understand

### Pipeline engine

`internal/pipeline/engine.go` defines:

```go
type Processor[T any] interface { Run(ctx context.Context, state *T) error }
func Execute[T any](ctx, state, []Processor[T]) error
type LoopProcessor[T any] struct { Body []Processor[T]; Until func(*T) bool; Max int }
```

Sentinel errors: `ErrIssueUnclear` (assess), `ErrNoChanges` (impl), `ErrLoopMax` (retry cap hit).

Each consumer builds a `[]Processor[StateT]` slice + calls `pipeline.Execute`. `LoopProcessor[T]` wraps the impl retry loop — body = `[BeginAttempt, RefreshFreshComments, ImplementAttempt, CheckHasChanges, RunTests]`, `Until = TestError==nil && Attempt>0`, `Max = 3`.

State types live in `internal/pipeline/state/state.go` — one strongly-typed struct per pipeline (`Assess`, `Impl`, `Review`). Processors in `internal/pipeline/{assess,impl,review}/`.

### Queue flow

```text
Forgejo → producer → queue:assess
                         ↓
                   assess-consumer ──(clear)──► queue:impl
                         ↓                          ↓
                   (unclear) post clarification     impl-consumer ──► queue:review
                                                                            ↓
                                                                      review-consumer
```

Payload structs: `models.AssessJob`, `models.ImplJob`, `models.ReviewJob`. Jobs carry enough context (owner/repo/issue number/title/body) so each consumer rebuilds state without cross-process session continuity — every Claude/Gemini call starts fresh (no `--resume`).

### Event flow

Claude's `--output-format stream-json` lines = source of truth for AI activity:

```text
claude subprocess stdout (NDJSON)
  → internal/agent/claude.go: runClaudeStream scans line-by-line
  → internal/events/parse.go: ParseClaudeLine(line) → []events.Event
  → events.Sink.Emit (injected per invocation)
  → runstore.AppendSink (XADD to run:{id}:events, INCR seq counter)
  → webui WebSocket handler (XRANGE replay → XREAD BLOCK tail)
  → React EventLog component
```

**Add new event kind → start at `internal/events/parse.go`.** Add a `Kind` constant + teach parser; rest of stack flows events through via generic `Event`.

### Shared `runstore.RunHandle`

`internal/runstore/handle.go` — one handle type for every pipeline. Owns run lifecycle, control subscription, notes mutex, seen-comments map. Constructor: `runstore.NewRunHandle(ctx, store, ctrl, initialRun)` — hooks up control, emits `KindRunStart`, returns a live handle. Methods: `SetPhase`, `AddUsage`, `SetAttempt`, `MarkCommentsSeen`, `FreshComments`, `DrainNotes`/`PrependNotes`, `Finalize`. Consumers defer `handle.Cancel()` + call `handle.Finalize(phase, err)` based on pipeline result.

### Token accounting

Claude `type=result` event has `usage` field (input/output/cache_creation/cache_read). Gemini returns `resp.UsageMetadata`. Both feed `events.TokenUsage`, accumulated per run via `RunHandle.AddUsage` → `runstore.Update`, flushed to `tokens:daily:{YYYY-MM-DD}` HASH on `store.Finalize`.

### Cancel / inject protocol

UI click → REST `POST /api/runs/{id}/cancel|inject` → Redis `PUBLISH ctrl:{runID}` → `RunHandle.handleControl` goroutine → `context.CancelFunc` fires → `exec.CommandContext` SIGKILLs Claude. Injected notes drained by `ImplementAttempt` Processor on next loop iteration + prepended to the next prompt.

### `internal/runtime`

Thin shared main.go boilerplate: `InitLogger`, `ShutdownContext` (SIGINT/SIGTERM), `NewStore/NewControl/NewQueue`, `MustEnv/EnvOr`, `ConfigureGit`, `CloseAll`, `Die`. Used by `cmd/producer`, `cmd/consumer`, `cmd/webui`.

## Project-specific gotchas

- **`cmd/webui/dist/index.html` must exist** even w/o frontend build, because `//go:embed all:dist` in `cmd/webui/assets.go` fails compilation if dir empty. `just clean` preserves it.
- **Vite outputs to `../cmd/webui/dist`** (configured in `web/vite.config.ts`), _not_ `web/dist`. So `go:embed` picks it up from the Go binary's own package.
- **`logRequest` middleware wraps `http.ResponseWriter` w/ `statusRecorder`**, must implement `http.Hijacker` for WebSocket upgrades. New wrapper → preserve Hijacker pass-through.
- **`coder/websocket` not `gorilla/websocket`** — small, context-aware, maintained.
- **Claude Code CLI auth** subscription-based via `config/claude/.credentials.json` baked into consumer image. No `ANTHROPIC_API_KEY` env var.
- **LSP diagnostics go stale** during big refactors (errors about old interface signature after regen mocks). Trust `go build ./...` + `go test ./...`, not IDE squiggles.
- **No cross-consumer session continuity.** Assess and impl run in different processes; `--resume sessionID` is never used. Impl rebuilds context from issue body + comments every time.

## Code style (project-specific additions to user's global CLAUDE.md)

- Agent/Gemini methods accept `events.Sink` as last param — every AI call must pipe events through the sink for UI display.
- Claude methods return `agent.AssessResult` or `agent.ImplementResult` (never bare tuples) — stays within the 2-return-values rule.
- Mocks generated w/ `moq -rm`, not handwritten. After interface change: `just generate`.
- Tests use explicit `t.Run` subtests, not loops over slice-of-struct fixtures.
- Each Processor lives in its own small struct; no monolithic `processIssue` / `runPipeline` functions.

## Where to start when asked to…

- **Add new Claude event kind**: `internal/events/{event.go,parse.go,parse_test.go}` → React `web/src/components/EventLog.tsx`.
- **Add new REST endpoint**: `cmd/webui/handlers.go` + route in `cmd/webui/server.go` + `cmd/webui/handlers_test.go`. Client in `web/src/api.ts`.
- **Add new run phase**: `internal/runstore/run.go` const block → emit via `handle.SetPhase` inside the relevant Processor → React styling in `web/src/styles.css` (`.phase-<name>` class).
- **Add a new pipeline stage**: add Processor in `internal/pipeline/<stage>/`, extend state struct in `internal/pipeline/state/state.go`, wire into the consumer's `[]Processor[T]` slice in `cmd/consumer/<stage>.go`.
- **Change Gemini prompts or token parsing**: `internal/gemini/*.go`. Usage emission via `emitUsage` helper in `gemini.go`.
- **Change Claude invocation args or timeout behavior**: `internal/agent/claude.go` — `runClaudeStream` = single entry point shared by `AssessIssue`, `ImplementIssue`, `prdesc.Generate`.
- **Add a new queue**: constant in `internal/queue/iface.go` + new job payload in `internal/models/models.go` + new Processor chain + new `--pipeline` branch in `cmd/consumer/main.go`.
