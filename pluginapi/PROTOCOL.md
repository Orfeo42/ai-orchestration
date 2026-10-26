# Plugin protocol v1

The orchestrator core manages workflows and processes only. Every capability (AI implementation, Forgejo, memory, notification, …) is a **plugin**: an independent HTTP service that implements this protocol. Implementing it is mandatory: the core refuses to register a plugin whose manifest does not validate, and every plugin must pass the conformance suite (`pluginapi/conformance`) in its own CI.

The machine-readable contract is `openapi.yaml`; the Go types are in this module (`github.com/Orfeo42/ai-orchestration/pluginapi`). Plugins written in Go use `server.Serve` (package `pluginapi/server`) and implement only `Action.Run`; the core and plugins call each other through package `pluginapi/client`; plugins in any other language implement the HTTP endpoints below.

## Concepts

- **Plugin**: one service, one manifest, one or more actions and triggers. Name pattern `^[a-z][a-z0-9-]*$` (e.g. `forgejo`, `ai`, `memory`, `notify`).
- **Action**: one capability of a plugin (e.g. `issue.read`, `pr.open`, `implement`). Name pattern `^[a-z][a-z0-9_.]*$`, unique within the plugin. A workflow step references it as `<plugin>/<action>`.
- **Outcome**: a business result an action declares in its manifest (pattern `^[a-z][a-z0-9_]*$`). The workflow branches on it. An action must declare at least one outcome; returning an undeclared one fails the step as `internal`.
- **Trigger**: an event a plugin emits to the core that can start processes (e.g. `issue.labeled`).
- **Job**: one execution of one action for one step run. `job_id` is the core's step run id and is the idempotency key.

## Deployment and registration

- Plugins are container images (from the monorepo or any external repo, published to a git registry) deployed by compose or Helm. The core never starts containers; it only knows each plugin's base URL and token.
- At registration and on refresh the core fetches `GET /v1/manifest` and validates it: `protocol` supported, names match their patterns, action names unique, every action has a `mode`, at least one outcome and valid JSON Schemas for input and output, capabilities known. A failing manifest means the plugin is not registered.
- When a workflow is saved, every `uses` must resolve to a registered action, every declared outcome must be handled, and the step's `with` must validate against the action's `input_schema` when it contains no expressions. `with` may reference earlier data with `${...}` expressions: a string that is exactly `${path}` is replaced by the JSON value at that path, `${path}` inside a longer string is interpolated (scalars only), `$${` escapes a literal `${`. The scope is `vars` (process vars), `subject` (`kind`, `key`, `url`, `title`) and `process` (`id`, `workflow`, `project`). After resolution the input is always validated against `input_schema` before the job starts; an unresolved expression or an invalid input fails the step without retry.

## Authentication

- Core → plugin: `Authorization: Bearer <plugin token>` on every request. The token is per plugin, provided to both sides as a secret (env var or Kubernetes Secret). Missing or wrong token → `401 unauthorized`. Authentication is checked before routing, so an unauthenticated request to an unknown path also gets `401`.
- Plugin → core, callbacks: the `result_url` and `progress_url` in the job request are opaque, per-job signed URLs. The plugin calls them as given, with no extra credentials.
- Plugin → core, triggers: `Authorization: Bearer <plugin token>` plus `X-Plugin-Name: <plugin name>`.

## Plugin endpoints (called by the core)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/manifest` | Identity, actions, triggers, schemas |
| `GET` | `/v1/health` | Liveness and dependency status |
| `POST` | `/v1/actions/{action}/jobs` | Start a job |
| `GET` | `/v1/jobs/{job_id}` | Job state and result |
| `POST` | `/v1/jobs/{job_id}/cancel` | Cancel a job |
| `GET` | `/v1/jobs/{job_id}/events` | Live event stream (SSE), only for actions with capability `events` |
| `POST` | `/v1/jobs/{job_id}/messages` | Inject a message into a running job, only for actions with capability `inject` |

### Start a job

Request body `JobRequest`: `job_id`, `process_id`, `input` (must validate against the action's `input_schema`), `callback.result_url`, `callback.progress_url`, `deadline` (RFC 3339), `heartbeat_seconds`.

- Sync action: the plugin runs the job within the request and replies `200` with the final `JobState` (status `succeeded` or `failed`, `result` set). Sync jobs never call `result_url` or `progress_url`; the response is the only delivery. The core's HTTP timeout is the action's `default_timeout_seconds` (or the step's timeout).
- Async action: the plugin accepts and replies `202` with `JobState` status `running`, then delivers the result to `result_url`.
- Idempotency: a repeated `job_id` with the same action and input returns the existing job's current state (`200` if finished, `202` if running) and never starts a second execution. The same `job_id` with a different action or input → `409 job_conflict`.
- Errors: unknown action → `404 unknown_action`; input not matching the schema → `422 invalid_input` (the core fails the step without retry); body over the size limit → `413 payload_too_large`; any `5xx` → the core retries the step according to its retry policy. A plugin that is shutting down refuses new jobs with `503 upstream_failure`.

### Job state, cancel

- `GET /v1/jobs/{job_id}` → `200 JobState`, or `404 job_not_found`. The core uses it to recover a lost callback or a missed heartbeat. Plugins keep finished job states at least 24 hours.
- `POST /v1/jobs/{job_id}/cancel` → `202 JobState` while the cancel is in progress, `200 JobState` if the job is already terminal (cancel is a no-op), `404 job_not_found`. A cancelled job ends with status `cancelled` and its result is still delivered to `result_url`. A job whose cancel arrives while `Run` is finishing is reported as `cancelled`, even if `Run` returned an outcome. Jobs interrupted by plugin shutdown are not delivered: the core detects them through missed heartbeats and `GET /v1/jobs/{job_id}` (`404` after a restart) and retries the step.

### Events (capability `events`)

`GET /v1/jobs/{job_id}/events?after=<seq>` returns `text/event-stream`. Each SSE message carries one `Event` as JSON in `data` (`seq` strictly increasing from 1, `at`, `kind`, `data`). The stream replays events with `seq > after`, then tails live events, and closes when the job is terminal. For a job that is already terminal the stream replays the stored events with `seq > after` and closes. Kinds are plugin-defined strings; the AI plugin uses the existing event kinds (`assistant_text`, `tool_use`, `tool_result`, `thinking`, `phase`, `result`, `error`, …).

### Messages (capability `inject`)

`POST /v1/jobs/{job_id}/messages` with `Message{text}` → `202` accepted, `409 job_not_active` if the job is terminal, `409 unsupported` if the action does not declare `inject`.

### Health

`GET /v1/health` → `200 Health` with `status` `ok`, `degraded` or `down` and optional per-dependency `checks`. A `down` plugin is not sent new jobs.

## Core endpoints (called by plugins)

| Method | URL | Purpose |
|---|---|---|
| `POST` | `callback.result_url` | Deliver the final `Result` (exactly once per job; retried until acknowledged) |
| `POST` | `callback.progress_url` | Heartbeat and phase (`Progress`) |
| `POST` | `/api/v1/triggers` | Emit a `TriggerEvent` |

### Result

`Result`: `job_id`, `outcome` (declared outcome; required when `error` is absent), `output` (JSON that validates against the action's `output_schema`, stored in the process vars under the step id, so later steps read it as `${vars.<step_id>...}`), `error` (`kind` `upstream` or `internal`, `message`), `usage` (token counts).

- `error` set means an infrastructure failure: `upstream` for a dependency fault (Forgejo, Claude, hub, timeouts), `internal` for the plugin's own fault. The core retries the step per its retry policy; after the last attempt the process fails with that `error_kind`.
- A business result that is not success is an outcome (e.g. `tests_failed`, `not_found`), never an error.
- Core replies: `204` acknowledged; `410 job_not_active` if the step run is no longer active (cancelled, superseded): the plugin stops retrying and drops the result. On network errors or `5xx` the plugin retries with exponential backoff until `deadline` plus 10 minutes. A fast async job can deliver its result before the core has finished recording the job; the core then answers `503 upstream_failure` and the retry delivers it.

### Progress and heartbeat

An async job sends `Progress{job_id, phase, message?, usage?, at}` to `progress_url` when its phase changes and at least every `heartbeat_seconds`. A job that waits for a human (e.g. an issue reply) keeps sending heartbeats with phase `waiting_for_input`. After three missed heartbeats the core calls `GET /v1/jobs/{job_id}`: `404` or no answer → the step fails as `upstream` and is retried per its policy. Core replies `204`, or `410` if the step run is no longer active (the plugin should cancel the job).

### Triggers

`POST /api/v1/triggers` with `TriggerEvent{plugin, trigger, subject{kind, key, url?, title?}, payload, dedup_key?, at}` → `202 TriggerAccepted{process_ids}` (possibly empty). The core starts every workflow whose trigger matches `<plugin>/<trigger>`; `payload` must validate against the trigger's `payload_schema` and becomes the process's initial var `trigger` (`${vars.trigger...}`). A workflow trigger is `plugin` + `event` with an optional `match`: every key is a dot path into the payload that must equal the given JSON value. The same `(workflow, subject.key)` never gets a second process, so re-emitting an event is safe.

## Limits and timing

- Request and response bodies: at most 16 MiB (`DefaultMaxBodyBytes`). Large state (e.g. a memory bundle) travels inline as base64 inside `input`/`output` until an artifact plugin exists.
- `deadline`: the plugin must stop the job by then and report `error.kind=internal` with a timeout message; the core enforces the same deadline on its side.
- No shared filesystem between plugins: each job works in its own scratch directory, deleted when the job ends. Code is shared through git (a job returns `branch` and `commit`), everything else through `output` → process vars → the next job's `input`.

## Error body

Every `4xx`/`5xx` response has the body `APIError{code, message}` with `code` one of `invalid_input`, `unknown_action`, `job_not_found`, `job_conflict`, `job_not_active`, `unsupported`, `unauthorized`, `payload_too_large`, `internal`, `upstream_failure`, `protocol_mismatch`.

## Versioning

`protocol` is `"1"`. Additive changes (new optional fields, new endpoints guarded by a capability) keep version 1; anything else bumps it. The core states which protocol versions it supports and rejects others with `protocol_mismatch`.
