# ai-orchestration

Durable, workflow-driven AI development pipeline. YAML workflows describe the steps; a trigger turns labeled Forgejo issues into processes (assess with Claude, ask for clarification, implement with tests, open a PR, review with Gemini, update the README); River workers execute the steps; a web UI shows live Claude output, history, analytics and lets you cancel, retry or inject instructions.

## Services

| Service         | Role                                                                                   |
| --------------- | -------------------------------------------------------------------------------------- |
| `postgres`      | Source of truth: workflow versions, processes, step runs, River jobs                   |
| `redis`         | Live event streams, cancel/inject pub/sub, worker registry (no durable state)          |
| `migrate`       | One-shot goose + River migrations                                                      |
| `trigger`       | Polls Forgejo per workflow trigger, starts processes, resumes comment waits            |
| `worker-claude` | River worker, `claude` queue: Claude CLI + git + Go toolchain (scalable)               |
| `worker-default`| River worker, `default` queue: tracker steps, no Claude                                |
| `webui`         | React SPA + REST/WebSocket: processes, workflows, live stream, analytics               |

## Flow

```text
Forgejo issue (label To-do, repo label repo/<x>)
  └─▶ trigger ──▶ process (Postgres, pinned to a workflow_version)
        └─▶ River job per step ──▶ worker
              start_labels ─▶ assess ─(unclear)─▶ ask ─(answered)─▶ assess
                               └─(clear)─▶ implement ─▶ describe ─▶ review ─▶ docs ─▶ label_done ─▶ end
```

`workflows/implement-issue.yaml` is the default workflow. Step outcomes select the next step; an error is retried by River and, when retries are exhausted, fails the process with an `error_kind` (`upstream`, `internal`, `business`). Steps can wait for an issue comment or an API signal.

## Quick start

```bash
cp .env.example .env
# fill POSTGRES_PASSWORD, FORGEJO_URL, FORGEJO_TOKEN, CLAUDE_CODE_OAUTH_TOKEN, MEMORY_*, GEMINI_API_KEY

cp projects.example.json projects.json
# adjust repos, base branches and labels

just build-up
```

Open http://127.0.0.1:8080. Give an issue in a repo listed in the workflow trigger the label `To-do` plus the project label from `projects.json` (for example `repo/billing-BE`) to start a process.

## Workflows

Files in `workflows/` (`*.yaml`):

```yaml
name: implement-issue
trigger:
  forgejo:
    repos: [ionstream/handbook]
    label: To-do
start: assess
steps:
  assess:
    uses: claude.assess
    timeout: 30m
    retry: 2
    on:
      clear: implement
      unclear: ask
```

- Every outcome a step declares needs an `on` entry, every `on` key must be a declared outcome, targets are step ids or `end` / `fail`, all steps must be reachable from `start`.
- The trigger syncs the directory into `workflow_version` (hashed by content). A process stays pinned to the version it started with, so edits only affect new processes. In compose `./workflows` is mounted into the trigger, no rebuild needed.
- Step catalog (`internal/steps`): `claude.assess`, `tracker.ask_clarification`, `claude.implement` (`max_attempts`), `pr.describe`, `ai.review` (`provider`: gemini or claude), `docs.readme`, `tracker.labels` (`add`, `remove`), `human.approve`.

## `projects.json`

Local, gitignored copy of `projects.example.json`. Maps a project key to its repo, base branch and the Forgejo repo label that routes an issue to it.

```json
{
  "billing-backend": {
    "repo": "ionstream/billing-backend",
    "base_branch": "develop",
    "label": "repo/billing-BE"
  }
}
```

Optional `memory_key` overrides the Claude memory project key (default `HOME-sviluppo-ionstream-<repo-name>`). A project without `label` can never be triggered.

## Commands (justfile)

```bash
just                       # list recipes
just build-up              # docker compose up --build -d
just down | start | restart | logs [service]
just migrate               # run migrations in docker
just worker-restart        # rebuild both workers
just trigger-restart
just webui-restart
just scale-claude 3        # claude worker replicas

# local dev (needs postgres + redis, e.g. the compose ones)
just run-migrate
just run-trigger
just run-worker            # queues="claude,default"; uses .cache/claude, never ~/.claude
just run-webui
just web-dev               # vite dev server on :5173, proxies /api /ws to :8080

# build + test
just build                 # frontend + go
just check                 # vet + test
just go-test               # unit tests, no docker
just go-test-integration   # includes integration tests, needs docker
just web-test | web-lint
just generate              # regen moq mocks
just clean
```

## Configuration

Full list in [`.env.example`](.env.example).

| Variable                    | Required        | Default                  | Purpose                                                   |
| --------------------------- | --------------- | ------------------------ | --------------------------------------------------------- |
| `POSTGRES_PASSWORD`         | yes             | —                        | Postgres password (compose builds `DATABASE_URL` from it) |
| `DATABASE_URL`              | yes (binaries)  | set by compose           | Postgres DSN                                              |
| `FORGEJO_URL`               | yes             | —                        | Forgejo instance URL                                      |
| `FORGEJO_TOKEN`             | yes             | —                        | API token, repo read/write scope                          |
| `REDIS_URL`                 | no              | `redis://redis:6379`     | Redis connection URL                                      |
| `POLL_INTERVAL`             | no              | `60s`                    | Trigger poll interval (Go duration)                       |
| `WORKFLOWS_DIR`             | no              | `/workflows`             | Trigger workflow directory                                |
| `PROJECTS_CONFIG`           | no              | `/config/projects.json`  | projects file inside the container                        |
| `PROJECTS_CONFIG_PATH`      | no              | `./projects.json`        | Host path mounted by compose                              |
| `CLAUDE_CODE_OAUTH_TOKEN`   | yes (claude)    | —                        | Subscription token from `claude setup-token`              |
| `MEMORY_SERVER_URL`         | yes (claude)    | —                        | claude-memory-sync hub URL                                |
| `MEMORY_TOKEN`              | yes (claude)    | —                        | claude-memory-sync hub bearer token                       |
| `GEMINI_API_KEY`            | no              | —                        | Gemini for review/docs/PR text; without it Claude is used |
| `CLAUDE_TIMEOUT`            | no              | `3600`                   | Per-invocation wall clock limit (seconds)                 |
| `DEFAULT_QUEUE_MAX_WORKERS` | no              | `4`                      | Concurrent steps on the `default` queue per worker        |
| `STEP_DEFAULT_TIMEOUT`      | no              | `2h`                     | Step timeout when the workflow sets none                  |
| `JOB_RESCUE_AFTER`          | no              | `3h`                     | River stuck-job rescue window, must exceed the timeout    |
| `GIT_USER_NAME` / `GIT_USER_EMAIL` | no       | `AI Orchestrator` / `ai@orchestrator.local` | Commit author                         |
| `LISTEN_ADDR`               | no              | `0.0.0.0:8080` (webui)   | Bind address for webui HTTP server                        |
| `TZ`                        | no              | `Europe/Rome`            | Container timezone                                        |

Claude Code CLI authenticates with `CLAUDE_CODE_OAUTH_TOKEN`; workers refuse to start if `ANTHROPIC_API_KEY` is set. Claude config and memory are mirrored from the claude-memory-sync hub when a claude worker starts and before every claude step. For local `just run-worker`, export `CLAUDE_CODE_OAUTH_TOKEN`, `MEMORY_SERVER_URL`, `MEMORY_TOKEN`, `FORGEJO_URL` and `FORGEJO_TOKEN` first.

## Forgejo labels

| Label             | Meaning                                                           |
| ----------------- | ----------------------------------------------------------------- |
| `To-do`           | Workflow trigger label (set in the workflow YAML)                 |
| `repo/<x>`        | Routes the issue to a project (`label` in `projects.json`)        |
| `in-progress`     | Set by the workflow while it works on the issue                   |
| `needs-info`      | Set when the workflow asked for clarification                     |
| `done`            | Set at the end; the trigger skips issues carrying it              |

## Web UI

- Process list / board with status filters, start dialog.
- Process detail: step timeline, live event log over WebSocket, diff, cancel / retry / signal, inject instructions into a running Claude step.
- Workflows: list and graph of each workflow version.
- Analytics: per-workflow success rate and duration, per-step outcomes and retries, token usage, failures by kind.

REST:

```text
GET  /api/processes            POST /api/processes
GET  /api/processes/{id}       POST /api/processes/{id}/{cancel,retry,signal}
GET  /api/step-runs/{id}       GET  /api/step-runs/{id}/{events,diff}
POST /api/step-runs/{id}/{cancel,inject}
GET  /api/workflows            GET  /api/workflows/{name}
GET  /api/analytics/{workflows,steps,tokens,failures}
GET  /healthz                  GET  /ws/step-runs/{id}  (WebSocket)
```

## Layout

```text
cmd/
  migrate/          goose + River migrations (Dockerfile)
  trigger/          Forgejo poller (Dockerfile)
  worker/           River worker, --queues=claude,default (Dockerfile, Claude CLI image)
  webui/            REST/WebSocket server + embedded React dist
workflows/          YAML workflow definitions
internal/
  workflow/         YAML schema, validation, loader
  step/, steps/     step contract and catalog
  engine/           transitions, retries, waits, cancel (River)
  process/          process / step_run model and Postgres store
  trigger/          Forgejo issue -> process
  analytics/        aggregate queries
  db/               connection + goose migrations
  agent/, gemini/   Claude CLI wrapper, Gemini clients
  events/, runstore/, control/  stream-json events, Redis streams, cancel/inject
  forgejo/, gitops/, testrunner/, memory/, projects/, runtime/
web/                React SPA (TypeScript, Vite)
```

## Testing

```bash
just go-test               # unit tests, no docker
just go-test-integration   # engine + store integration tests (Docker required)
just web-test
```

## License

Apache License 2.0, see [LICENSE](LICENSE).
