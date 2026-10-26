# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

A durable workflow engine for AI-driven development work. Workflows (stored in Postgres, edited in the UI, seeded from `workflows/*.yaml`) describe a graph of steps; triggers (Forgejo polling or plugin events) turn subjects into processes; River workers execute the steps, either compiled-in steps or actions of HTTP plugins (`pluginapi`); a web UI shows and controls everything live.

- **Postgres** is the source of truth: `workflow_version` (immutable versions), `workflow` (name → current version), `process`, `step_run`, `plugin` (registry) tables (goose migrations in `internal/db/migrations`) plus River's own tables. A **process** is one run of a workflow for one subject (a Forgejo issue); a **step_run** is one attempt of one step.
- **Redis** only holds ephemeral/live data: stream-json event streams per step run, control pub/sub (cancel/inject), the worker registry. Losing Redis loses live logs, never process state.
- **`cmd/migrate`** one-shot: goose migrations + River migrations (`DATABASE_URL`). Runs before everything else (compose `migrate` service).
- **`cmd/trigger`** polls Forgejo for each workflow's trigger (`trigger.forgejo`: repos + label), maps the issue to a project via `projects.json` `label`, starts a process, and resumes processes waiting on an issue comment. On start it imports `WORKFLOWS_DIR` files only for workflow names not in the DB yet (after that the DB, edited from the UI, is the source). Every poll it also refreshes plugin manifests/health and runs the plugin job watchdog.
- **`cmd/worker`** River worker, `--queues=claude,default`. The `claude` queue runs one step at a time per worker (Claude CLI + git + Go toolchain for tests, memory mirror); the `default` queue runs `DEFAULT_QUEUE_MAX_WORKERS` concurrent steps (tracker calls, no Claude).
- **`cmd/webui`** embedded React SPA + REST/WebSocket server (`127.0.0.1:8080`) and the core API plugins call (job callbacks, triggers). Needs `DATABASE_URL`, `CALLBACK_SECRET`, `FORGEJO_URL`, `FORGEJO_TOKEN`, `PROJECTS_CONFIG`, `REDIS_URL`.
- **Plugin env**: worker needs `CORE_PUBLIC_URL` (base URL plugins reach the webui at, compose `http://webui:8080`) and `CALLBACK_SECRET` (HMAC key of callback URLs, same value in webui); every binary that talks to plugins reads `PLUGIN_<NAME>_TOKEN` (`-` → `_`, upper case; never stored in the DB) and `PLUGIN_REFRESH_INTERVAL` (catalog reload, default `30s`).

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

CI is `.github/workflows/ci.yml` (GitHub Actions): `go` (gofmt, vet, unit tests, `pluginapi` and `plugins/echo` tests with `-race`), `go-integration` (full `go test`, testcontainers on the runner's Docker), `web` (lint, typecheck, Vitest, build) on every push to `main` and every PR. `.github/workflows/publish.yml` runs on a published GitHub release (tag `vX.Y.Z`): it first runs `ci.yml` as a reusable workflow (`workflow_call`) on the release ref, and only when every CI job passes (`needs: [ci]`) it builds `linux/amd64` images `ghcr.io/orfeo42/ai-orchestration-{worker,trigger,migrate,webui,plugin-echo}` tagged `X.Y.Z`, `X.Y` and `latest` (prereleases get only `X.Y.Z`). Merging to `main` publishes nothing. New deployable components (plugins) get a matrix entry in `publish.yml`. Dependabot (`.github/dependabot.yml`) opens one grouped weekly PR per ecosystem: Go modules (root, `pluginapi`, `plugins/echo`, `.tools/*`), npm (`web`), GitHub Actions, Dockerfile base images, compose images.

## Plugins

`pluginapi/` is a separate Go module (`github.com/Orfeo42/ai-orchestration/pluginapi`, wired through `go.work`) holding the plugin protocol v1: `PROTOCOL.md` (spec), `openapi.yaml` (HTTP contract), Go types (`Manifest`, `ActionSpec`, `JobRequest`, `Result`, `Progress`, `TriggerEvent`, …), the `Action`/`Reporter` interfaces, schema helpers (`CompileSchema`, `SchemaFor`), and subpackages `client` (`Plugin`: core → plugin, `Core`: plugin → core), `server` (`New`/`Serve`: the HTTP side every Go plugin uses) and `conformance` (`Run`: the mandatory suite). Target: the core only manages workflows and processes; AI, Forgejo, memory and notification become plugins that import `pluginapi` only. Plugins never import the core. Lint the spec with `npx @redocly/cli lint pluginapi/openapi.yaml`.

Core side, `internal/plugins`:
- `Catalog` implements `step.Catalog`: compiled steps from `*step.Registry` plus every registered plugin action as `<plugin>/<action>` (queue `default`), from an in-memory snapshot of the `plugin` table (`Refresh`; binaries reload it every `PLUGIN_REFRESH_INTERVAL`). Engine, workflow validation and the editor catalog all read it.
- `Registry` (register/refresh/remove a plugin: fetch + validate manifest, health → status; remove refused while a current workflow uses it), `Clients` (token lookup, constant-time trigger auth, job cancel), `Signer` (HMAC-signed per-job callback URLs `/api/v1/jobs/{id}/{result,progress}?exp&sig`), `Callbacks` (validate a result against the action contract, hand it to `engine.CompleteJob` / `JobProgress`), `Watchdog` (stale or expired jobs → `GET /v1/jobs/{id}` → deliver, touch heartbeat or fail upstream).
- Plugin step (`step.go`): resolves `with` expressions (`internal/template`: `${vars.<step>.x}`, `${subject.key}`, `${process.id}`; whole-string expression keeps the JSON type), validates input, calls `StartJob` (job id = step run id). Sync → result in place; async → job wait.
- `plugins/echo` is the reference plugin (own module, imports only `pluginapi`, passes `conformance`): sync `echo`, async `wait`, trigger `ping`. Compose profile `echo` (`docker compose --profile echo up`), token `PLUGIN_ECHO_TOKEN`, register it in the UI with base URL `http://echo:8090`.

## Architecture that requires multiple files to understand

### Workflows

`workflows/*.yaml`, schema and validation in `internal/workflow/workflow.go` (`Parse` YAML, `ParseJSON`, `Definition.YAML()` round-trip, `Validate(step.Catalog)`). Strict: unknown fields rejected, every outcome a step declares must have an `on` entry, every `on` key must be a declared outcome, every `on` target must be a step or `end`/`fail`, every step reachable from `start`, `end`/`fail` are reserved ids. Trigger: none (manual start only), `forgejo` (repos as `owner/repo` + label), or `plugin` + `event` (must exist in the catalog) + optional `match` (dot path into the payload → expected JSON value); never both. Step fields: `description`, `uses`, `with` (compiled steps: validated against the params struct; plugin actions: against `input_schema` when expression-free), `timeout`, `retry`, `on`.

Every save (`POST /api/workflows` from the editor) validates against the catalog, stores a new `workflow_version` (hash = sha256 of the generated YAML) and points `workflow.current_version_id` at it in one tx; `DELETE /api/workflows/{name}` drops the `workflow` row and keeps the versions. `trigger.Service.SyncWorkflows` only seeds names missing from `workflow`, so editing a YAML file after the first import has no effect (delete the workflow in the UI to re-import it). A process is **pinned** to the `workflow_version` it started with: edits only affect processes started afterwards. `LatestWorkflowVersions` = the current versions.

### Steps

`internal/step/step.go` defines `Def` (name, description, queue, declared outcomes, input schema and plugin for plugin actions), the `Catalog` interface (`Lookup`, `Build`, `Defs`, `HasTrigger`), `Registry` (compiled steps), `Step` (`Run(ctx, step.Context) (step.Result, error)`) and `Result` (`Outcome`, `Vars` merged into process vars, optional `Wait`). The catalog is `internal/steps/deps.go` `RegisterAll`: `claude.assess`, `tracker.ask_clarification`, `claude.implement`, `pr.describe`, `ai.review`, `docs.readme`, `tracker.labels`, `human.approve`. Step logic reuses `internal/pipeline/*` processors and `internal/agent`, `internal/gemini`, `internal/gitops`. Dependencies are injected through `steps.Deps`; trigger/webui register with empty `Deps{}` (they only need names and outcomes), workers with real ones.

### Engine semantics

`internal/engine`: a step run is a River job (`StepArgs`) on the step's queue.
- Step returns an outcome → `workflow.Definition.Next(step, outcome)` → next step is enqueued in the same transaction that records the step_run result, merges its vars and completes the River job, or the process ends (`end` → succeeded, `fail` → failed with `error_kind=business`). An outcome the step did not declare is an internal failure.
- Step returns an error → the step_run is recorded `failed` and River retries the same job (`MaxAttempts = retry + 1`, each attempt is a new step_run). After the last attempt the process becomes `failed` with `error` + `error_kind`: `upstream` when the error wraps `step.ErrUpstream` (Forgejo/Claude/Gemini failures, logged at Warn), otherwise `internal` (logged at Error). Worker shutdown or River timeout mid-step records the step_run `failed` and leaves the job to River's retry/rescue.
- Step returns an error wrapping `step.ErrPermanent` (bad input, unresolved expression, plugin contract violation) → the process fails at once as `internal`, no retry.
- Async plugin action → the step returns a `job` wait: the step_run stays `running`, the process goes `waiting` with `waiting_for.job` (step run id, plugin, attempt, heartbeat, deadline), the River job completes. `engine.CompleteJob` (result callback or watchdog) closes the step run and, in one tx, stores the output under `vars.<step_id>` and advances; an error result is retried with a new River job (`StepArgs.AttemptBase` keeps one attempt budget of `retry + 1` across sync and async attempts), the last one fails the process with its `error_kind`. `engine.JobProgress` (progress callback) touches `step_run.heartbeat_at`, phase and usage; once the step run is closed both return `ErrNotActive` (→ `410` to the plugin). Cancel of a job-waiting process cancels the step run and calls the plugin's `cancel`.
- Step returns a `Wait` (`comment` or `signal`) → process goes `waiting`. `comment`: the trigger's `ResumeWaiting` resumes it when a comment by someone other than the bot appears after the wait started. `signal`: `POST /api/processes/{id}/signal` resumes it with the given outcome. The resume outcome picks the next transition.
- Cancel (`POST /api/processes/{id}/cancel`) marks the process `cancelled`, cancels its River job (River cancels the job context remotely, the worker records the step_run `cancelled`) and publishes on Redis `ctrl:{stepRunID}` so the running Claude subprocess is killed. Retry (`POST .../retry` with optional `step`, default the current step) re-enqueues a failed/cancelled process. Inject (`POST /api/step-runs/{id}/inject`) appends a note and interrupts only the current Claude call (`RunHandle.Interruptible`); the implement loop re-runs that attempt with the note, the step keeps running.
- Process statuses: `pending running waiting succeeded failed cancelled`. At most one process ever per `(workflow_name, subject_key)`: `CreateProcess` skips a subject that already has a process in any status (the partial unique index on active processes guards the concurrent-insert race). A failed or cancelled process is never re-triggered by polling; restart it with retry.
- `STEP_DEFAULT_TIMEOUT` applies when a step sets no `timeout`; `JOB_RESCUE_AFTER` (must be greater) is River's stuck-job rescue window.

### Trigger

`internal/trigger`: `StartFromEvent` (behind `POST /api/v1/triggers`, plugin bearer token + `X-Plugin-Name`, payload validated against the trigger's `payload_schema`) starts every current workflow whose `plugin`/`event`/`match` fits, with the payload as `vars.trigger`. Forgejo polling: for each current workflow version and each repo in `trigger.forgejo.repos`, list issues with the trigger label (optionally filtered by `assignee`), skip issues already labeled `done`, map the issue to a project key through `projects.json` `label` (`repo/billing-BE` → `billing-backend`; no match → warning, skipped), and start a process with the issue as subject (`owner/repo#number`; re-polling is idempotent because a subject never gets a second process for the same workflow). Projects without `label` can never be triggered (warned at start).

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

REST (`cmd/webui/server.go`): `GET/POST /api/processes`, `GET /api/processes/{id}`, `POST /api/processes/{id}/{cancel,retry,signal}`, `GET /api/step-runs/{id}` + `/events` + `/diff`, `POST /api/step-runs/{id}/{cancel,inject}`, `GET /api/workflows[/{name}]`, `POST /api/workflows`, `DELETE /api/workflows/{name}`, `GET /api/catalog` (steps + plugin triggers for the editor), `GET/POST /api/plugins`, `POST /api/plugins/{name}/refresh`, `DELETE /api/plugins/{name}`, `GET /api/analytics/{workflows,steps,tokens,failures}`, `GET /healthz`, WebSocket `GET /ws/step-runs/{id}` (`process_handlers.go`, `plugin_handlers.go`). Plugin-facing (`pluginapi.APIError` bodies): `POST /api/v1/jobs/{id}/{result,progress}` (signed URL), `POST /api/v1/triggers`. Pages (`web/src/pages`): process list/board, process detail (step timeline, live event log, cancel/retry/inject/signal, diff), workflow list/detail (graph, edit, delete), workflow editor (`lib/workflowEditor.ts` holds the definition ⇄ form state logic), plugins, analytics.

### Cancel / inject protocol

UI click → REST → Redis `PUBLISH ctrl:{stepRunID}` → `RunHandle.handleControl` goroutine → `context.CancelFunc` fires → `exec.CommandContext` SIGKILLs Claude. Injected notes drained by the implement step on next loop iteration and prepended to the next prompt.

### `internal/runtime`

Shared main.go boilerplate: `InitLogger`, `ShutdownContext` (SIGINT/SIGTERM), `NewStore/NewControl`, `OpenPlatform` (Postgres + River + plugin catalog/clients/signer + engine wiring; `RefreshPlugins` loop), `MustEnv/EnvOr`, `ConfigureGit`, `CloseAll`, `Die`.

## Project-specific gotchas

- **`cmd/webui/dist/index.html` must exist** even w/o frontend build, because `//go:embed all:dist` in `cmd/webui/assets.go` fails compilation if dir empty. `just clean` preserves it.
- **Vite outputs to `../cmd/webui/dist`** (configured in `web/vite.config.ts`), _not_ `web/dist`. So `go:embed` picks it up from the Go binary's own package.
- **No request-logging middleware** in webui (it logged every request at Debug). Any future `http.ResponseWriter` wrapper must pass `http.Hijacker` through or WebSocket upgrades break.
- **Root `go.mod` requires `pluginapi v0.0.0` with `replace => ./pluginapi`**, so builds work with and without `go.work`; Dockerfiles copy `pluginapi/go.mod` + `go.sum` before `go mod download`. `plugins/*` modules do the same with `../../pluginapi` and build with `GOWORK=off`.
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
- **Add or change a workflow**: in the UI editor (Workflows → New/Edit), or a new YAML file in `workflows/` for a name not in the DB yet (validated by `workflow.LoadDir`, see `internal/workflow/load_test.go`). Running processes keep their pinned version.
- **Add a plugin**: new module under `plugins/<name>` importing only `pluginapi` (copy `plugins/echo`: actions implement `pluginapi.Action`, `server.Serve`, `conformance.Run` test), a Dockerfile, a compose service, a `publish.yml` matrix entry and a `dependabot.yml` gomod directory; then set `PLUGIN_<NAME>_TOKEN` on both sides and register it in the UI.
- **Change the DB schema**: new goose migration in `internal/db/migrations` (idempotent up and down), then the store in `internal/process/postgres` and, if analytics are affected, `internal/analytics/postgres`.
- **Add new Claude event kind**: `internal/events/{event.go,parse.go,parse_test.go}` → React `web/src/components/EventLog.tsx`.
- **Add new REST endpoint**: `cmd/webui/process_handlers.go` (or `handlers.go`) + route in `cmd/webui/server.go` + handler test. Client in `web/src/api.ts`.
- **Change Gemini prompts or token parsing**: `internal/gemini/*.go`. Usage emission via `emitUsage` helper in `gemini.go`.
- **Change Claude invocation args or timeout behavior**: `internal/agent/claude.go` — `baseArgs` builds the shared CLI args (tool list per call site, overlay flag from `Runtime.SystemPromptFile`); `runClaudeStream` = single entry point shared by assess, implement and PR description.
- **Change engine transitions, retries or waits**: `internal/engine/engine.go` and `worker.go`; integration coverage in `internal/engine/integration_test.go` (`just go-test-integration ./internal/engine/...`). Plugin job path end to end (trigger → sync + async plugin steps → callbacks → retry/cancel, real Postgres + River + `pluginapi/server`): `cmd/webui/e2e_test.go`.
