# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

A durable workflow engine for AI-driven development work. YAML workflows (`workflows/`) describe a graph of steps; a trigger turns Forgejo issues into processes; River workers execute the steps; a web UI shows and controls everything live.

- **Postgres** is the source of truth: `workflow_version`, `process`, `step_run` tables (goose migrations in `internal/db/migrations`) plus River's own tables. A **process** is one run of a workflow for one subject (a Forgejo issue); a **step_run** is one attempt of one step.
- **Redis** only holds ephemeral/live data: stream-json event streams per step run, control pub/sub (cancel/inject), the worker registry. Losing Redis loses live logs, never process state.
- **`cmd/migrate`** one-shot: goose migrations + River migrations (`DATABASE_URL`). Runs before everything else (compose `migrate` service).
- **`cmd/trigger`** polls Forgejo for each workflow's trigger (`trigger.forgejo`: repos + label), maps the issue to a project via `projects.json` `label`, starts a process, and resumes processes waiting on an issue comment. Syncs `WORKFLOWS_DIR` into `workflow_version` on start and every poll.
- **`cmd/worker`** River worker, `--queues=claude,default`. The `claude` queue runs one step at a time per worker (Claude CLI + git + Go toolchain for tests, memory mirror); the `default` queue runs `DEFAULT_QUEUE_MAX_WORKERS` concurrent steps (tracker calls, no Claude).
- **`cmd/webui`** embedded React SPA + REST/WebSocket server (`127.0.0.1:8080`). Needs `DATABASE_URL`, `FORGEJO_URL`, `FORGEJO_TOKEN`, `PROJECTS_CONFIG`, `REDIS_URL`.

Scale claude workers with `just scale-claude n=3` (each replica mirrors the Claude memory on its own start; no shared `/state/memory` volume).

## Build / test / run

Use `justfile` — every workflow has a recipe (`just --list`).

- `just build-up` — `docker compose up --build -d` (postgres, redis, migrate, trigger, worker-claude, worker-default, webui). Needs `.env` (copy `.env.example`) and `projects.json` (copy `projects.example.json`).
- `just migrate` / `just run-migrate` — apply migrations in docker / against the local postgres (`127.0.0.1:5432`, published by compose).
- `just run-{migrate,trigger,worker,webui}` — run one binary locally. `run-worker queues="claude,default"` points `CLAUDE_CONFIG_DIR` at `.cache/claude`, `MEMORY_STATE_DIR` at `.cache/memory` and `PROJECTS_ROOT` at `.cache/projects`, so a local run never touches `~/.claude`. Export `CLAUDE_CODE_OAUTH_TOKEN`, `MEMORY_SERVER_URL`, `MEMORY_TOKEN`, `FORGEJO_URL`, `FORGEJO_TOKEN` first.
- `just worker-restart`, `just trigger-restart`, `just webui-restart` — rebuild + restart one service group.
- `just web-dev` — Vite dev server on `:5173`, proxies `/api` + `/ws` to `:8080`. `just web-test`, `just web-lint` — Vitest / ESLint.
- `just go-test` — `go test -short ./...` (unit tests, miniredis, no docker).
- `just go-test-integration pkg="./..."` — full `go test -count=1`, includes integration tests (Postgres/River via Docker; needs a running Docker daemon).
- `just generate` — regen moq mocks after interface edits. `just check` — vet + test.

Single test: `go test -run TestParseClaudeLine/assistant_tool_use ./internal/events/...`.

CI is `.github/workflows/ci.yml` (GitHub Actions): `go` (gofmt, vet, unit tests, `pluginapi` tests with `-race`), `go-integration` (full `go test`, testcontainers on the runner's Docker), `web` (lint, typecheck, Vitest, build) on every push and PR; on `main` the `publish` job builds `linux/amd64` images `ghcr.io/orfeo42/ai-orchestration-{worker,trigger,migrate,webui}` tagged `latest` + `sha-<commit>`. New deployable components (plugins) get a matrix entry there. Dependabot (`.github/dependabot.yml`) opens one grouped weekly PR per ecosystem: Go modules (root, `pluginapi`, `.tools/*`), npm (`web`), GitHub Actions, Dockerfile base images, compose images.

## Plugin contract (in progress)

`pluginapi/` is a separate Go module (`github.com/Orfeo42/ai-orchestration/pluginapi`, wired through `go.work`) holding the plugin protocol v1: `PROTOCOL.md` (spec), `openapi.yaml` (HTTP contract), Go types (`Manifest`, `ActionSpec`, `JobRequest`, `Result`, `Progress`, `TriggerEvent`, …), the `Action`/`Reporter` interfaces, schema helpers (`CompileSchema`, `SchemaFor`), and subpackages `client` (`Plugin`: core → plugin, `Core`: plugin → core), `server` (`New`/`Serve`: the HTTP side every Go plugin uses) and `conformance` (`Run`: the mandatory suite). Target: the core only manages workflows and processes; AI, Forgejo, memory and notification become plugins that import `pluginapi` only. Plugins never import the core. Lint the spec with `npx @redocly/cli lint pluginapi/openapi.yaml`.

## Architecture that requires multiple files to understand

### Workflows

`workflows/*.yaml`, schema and validation in `internal/workflow/workflow.go`. Strict: unknown fields rejected, every outcome a step declares must have an `on` entry, every `on` key must be a declared outcome, every `on` target must be a step or `end`/`fail`, every step reachable from `start`, `end`/`fail` are reserved ids, `trigger.forgejo` (repos as `owner/repo` + label) required. Step fields: `uses`, `with` (params, validated against the step's params struct), `timeout`, `retry`, `on`.

Each file is hashed (sha256 of the YAML) and stored in `workflow_version` (`name`, `hash`, definition JSON, source YAML) by `trigger.Service.SyncWorkflows`. A process is **pinned** to the `workflow_version` it started with: editing a YAML file only affects processes started afterwards. Running `workflows/` through `workflow.LoadDir(dir, reg)` against `steps.RegisterAll(reg, steps.Deps{})` is the full validation.

### Steps

`internal/step/step.go` defines `Def` (name, queue, declared outcomes), `Registry`, `Step` (`Run(ctx, step.Context) (step.Result, error)`) and `Result` (`Outcome`, `Vars` merged into process vars, optional `Wait`). The catalog is `internal/steps/deps.go` `RegisterAll`: `claude.assess`, `tracker.ask_clarification`, `claude.implement`, `pr.describe`, `ai.review`, `docs.readme`, `tracker.labels`, `human.approve`. Step logic reuses `internal/pipeline/*` processors and `internal/agent`, `internal/gemini`, `internal/gitops`. Dependencies are injected through `steps.Deps`; trigger/webui register with empty `Deps{}` (they only need names and outcomes), workers with real ones.

### Engine semantics

`internal/engine`: a step run is a River job (`StepArgs`) on the step's queue.
- Step returns an outcome → `workflow.Definition.Next(step, outcome)` → next step is enqueued in the same transaction that records the step_run result, merges its vars and completes the River job, or the process ends (`end` → succeeded, `fail` → failed with `error_kind=business`). An outcome the step did not declare is an internal failure.
- Step returns an error → the step_run is recorded `failed` and River retries the same job (`MaxAttempts = retry + 1`, each attempt is a new step_run). After the last attempt the process becomes `failed` with `error` + `error_kind`: `upstream` when the error wraps `step.ErrUpstream` (Forgejo/Claude/Gemini failures, logged at Warn), otherwise `internal` (logged at Error). Worker shutdown or River timeout mid-step records the step_run `failed` and leaves the job to River's retry/rescue.
- Step returns a `Wait` (`comment` or `signal`) → process goes `waiting`. `comment`: the trigger's `ResumeWaiting` resumes it when a comment by someone other than the bot appears after the wait started. `signal`: `POST /api/processes/{id}/signal` resumes it with the given outcome. The resume outcome picks the next transition.
- Cancel (`POST /api/processes/{id}/cancel`) marks the process `cancelled`, cancels its River job (River cancels the job context remotely, the worker records the step_run `cancelled`) and publishes on Redis `ctrl:{stepRunID}` so the running Claude subprocess is killed. Retry (`POST .../retry` with optional `step`, default the current step) re-enqueues a failed/cancelled process. Inject (`POST /api/step-runs/{id}/inject`) appends a note and interrupts only the current Claude call (`RunHandle.Interruptible`); the implement loop re-runs that attempt with the note, the step keeps running.
- Process statuses: `pending running waiting succeeded failed cancelled`. At most one process ever per `(workflow_name, subject_key)`: `CreateProcess` skips a subject that already has a process in any status (the partial unique index on active processes guards the concurrent-insert race). A failed or cancelled process is never re-triggered by polling; restart it with retry.
- `STEP_DEFAULT_TIMEOUT` applies when a step sets no `timeout`; `JOB_RESCUE_AFTER` (must be greater) is River's stuck-job rescue window.

### Trigger

`internal/trigger`: for each latest workflow version and each repo in `trigger.forgejo.repos`, list issues with the trigger label (optionally filtered by `assignee`), skip issues already labeled `done`, map the issue to a project key through `projects.json` `label` (`repo/billing-BE` → `billing-backend`; no match → warning, skipped), and start a process with the issue as subject (`owner/repo#number`; re-polling is idempotent because a subject never gets a second process for the same workflow). Projects without `label` can never be triggered (warned at start).

### Event flow

Claude's `--output-format stream-json` lines = source of truth for AI activity:

```text
claude subprocess stdout (NDJSON)
  → internal/agent/claude.go: runClaudeStream scans line-by-line
  → internal/events/parse.go: ParseClaudeLine(line) → []events.Event
  → events.Sink.Emit (injected per invocation)
  → runstore step handle (XADD to Redis, token usage persisted on step_run)
  → webui WebSocket handler (XRANGE replay → XREAD BLOCK tail)
  → React EventLog component
```

**Add new event kind → start at `internal/events/parse.go`.** Add a `Kind` constant + teach parser; rest of stack flows events through via generic `Event`.

### Token accounting and analytics

Claude `type=result` has `usage` (input/output/cache_creation/cache_read); Gemini returns `resp.UsageMetadata`. Both feed `events.TokenUsage`, accumulated per step run and stored on the `step_run` row. `internal/analytics` (Postgres implementation in `internal/analytics/postgres`) aggregates per workflow (success rate, p50/p95 duration), per step (outcomes, retries, failure rate), tokens (group by day/project/step) and failures (by `error_kind`).

### Web UI

REST (`cmd/webui/server.go`): `GET/POST /api/processes`, `GET /api/processes/{id}`, `POST /api/processes/{id}/{cancel,retry,signal}`, `GET /api/step-runs/{id}` + `/events` + `/diff`, `POST /api/step-runs/{id}/{cancel,inject}`, `GET /api/workflows[/{name}]`, `GET /api/analytics/{workflows,steps,tokens,failures}`, `GET /healthz`, WebSocket `GET /ws/step-runs/{id}`. Pages (`web/src/pages`): process list/board, process detail (step timeline, live event log, cancel/retry/inject/signal, diff), workflow list/detail (graph), analytics.

### Cancel / inject protocol

UI click → REST → Redis `PUBLISH ctrl:{stepRunID}` → `RunHandle.handleControl` goroutine → `context.CancelFunc` fires → `exec.CommandContext` SIGKILLs Claude. Injected notes drained by the implement step on next loop iteration and prepended to the next prompt.

### `internal/runtime`

Shared main.go boilerplate: `InitLogger`, `ShutdownContext` (SIGINT/SIGTERM), `NewStore/NewControl`, `OpenPlatform` (Postgres + River + engine wiring), `MustEnv/EnvOr`, `ConfigureGit`, `CloseAll`, `Die`.

## Project-specific gotchas

- **`cmd/webui/dist/index.html` must exist** even w/o frontend build, because `//go:embed all:dist` in `cmd/webui/assets.go` fails compilation if dir empty. `just clean` preserves it.
- **Vite outputs to `../cmd/webui/dist`** (configured in `web/vite.config.ts`), _not_ `web/dist`. So `go:embed` picks it up from the Go binary's own package.
- **`logRequest` middleware wraps `http.ResponseWriter` w/ `statusRecorder`**, must implement `http.Hijacker` for WebSocket upgrades. New wrapper → preserve Hijacker pass-through.
- **`coder/websocket` not `gorilla/websocket`** — small, context-aware, maintained.
- **Claude Code CLI auth** via env `CLAUDE_CODE_OAUTH_TOKEN` (subscription token from `claude setup-token`). Workers refuse to start if `ANTHROPIC_API_KEY` is set (would bill API credits); the `claude` queue also requires the token.
- **Claude memory** (`CLAUDE.md`, rules, skills, agents, per-project memory) is mirrored read-only from the claude-memory-sync hub (`MEMORY_SERVER_URL`/`MEMORY_TOKEN`) by `internal/memory` at claude-worker start and before every claude step (hub down: warn, last mirror kept). Headless overlay `config/claude/headless.md` is appended via `--append-system-prompt-file` (`CLAUDE_SYSTEM_PROMPT_FILE`). Per-project memory key = `projects.json` `memory_key`, default `HOME-sviluppo-ionstream-<repo-name>`.
- **`projects.json` is local and gitignored**; `projects.example.json` is the tracked template. Each project needs `label` (the Forgejo repo label the trigger matches).
- **Migrations must stay idempotent** (`IF NOT EXISTS` / `IF EXISTS`), up and down.
- **LSP diagnostics go stale** during big refactors (errors about old interface signature after regen mocks). Trust `go build ./...` + `go test ./...`, not IDE squiggles.
- **No cross-step session continuity.** Steps run in different processes; `--resume sessionID` is never used. Each step rebuilds context from issue body + comments + process vars.

## Code style (project-specific additions to user's global CLAUDE.md)

- Agent/Gemini methods accept `events.Sink` as last param — every AI call must pipe events through the sink for UI display.
- Claude methods return `agent.AssessResult` or `agent.ImplementResult` (never bare tuples) — stays within the 2-return-values rule.
- Mocks generated w/ `moq -rm`, not handwritten. After interface change: `just generate`.
- Tests use explicit `t.Run` subtests, not loops over slice-of-struct fixtures.
- Each step lives in its own small struct in `internal/steps`; no monolithic run functions.

## Where to start when asked to…

- **Add a step**: new file in `internal/steps` (params struct with `validate` tags, struct implementing `step.Step`, outcome constants) + register it in `RegisterAll` (`internal/steps/deps.go`) with its queue and declared outcomes + tests + use it from a workflow YAML.
- **Add or change a workflow**: edit/add a YAML file in `workflows/`; validate via `workflow.LoadDir` (see `internal/workflow/load_test.go`). Running processes keep their pinned version.
- **Change the DB schema**: new goose migration in `internal/db/migrations` (idempotent up and down), then the store in `internal/process/postgres` and, if analytics are affected, `internal/analytics/postgres`.
- **Add new Claude event kind**: `internal/events/{event.go,parse.go,parse_test.go}` → React `web/src/components/EventLog.tsx`.
- **Add new REST endpoint**: `cmd/webui/process_handlers.go` (or `handlers.go`) + route in `cmd/webui/server.go` + handler test. Client in `web/src/api.ts`.
- **Change Gemini prompts or token parsing**: `internal/gemini/*.go`. Usage emission via `emitUsage` helper in `gemini.go`.
- **Change Claude invocation args or timeout behavior**: `internal/agent/claude.go` — `baseArgs` builds the shared CLI args (tool list per call site, overlay flag from `Runtime.SystemPromptFile`); `runClaudeStream` = single entry point shared by assess, implement and PR description.
- **Change engine transitions, retries or waits**: `internal/engine/engine.go` and `worker.go`; integration coverage in `internal/engine/integration_test.go` (`just go-test-integration ./internal/engine/...`).
