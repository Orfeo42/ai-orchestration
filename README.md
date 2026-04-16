# ai-orchestration

AI-powered development pipeline. Polls Forgejo issues, implements them with Claude Code, opens PRs, reviews them with Gemini, and exposes a live web UI for observation + control.

## Services

| Service        | Role                                                    | AI                 |
| -------------- | ------------------------------------------------------- | ------------------ |
| `orchestrator` | Poll issues → assess → implement → test → open PR       | Claude Code CLI    |
| `reviewer`     | Consume `queue:review` → review diff → update README    | Gemini 2.0 Flash   |
| `webui`        | React SPA + REST/WebSocket — history, live stream, stop/inject, tokens | —        |
| `redis`        | Event streams, run history, control pub/sub, job queue  | —                  |

## Flow

```
Forgejo issue (label: ai-implement)
  └─▶ orchestrator
        ├─ AssessIssue (Claude) → clear? yes/no
        ├─ ImplementIssue (Claude) with pinned --session-id
        ├─ run tests → retry up to 3× (feeds test output back into next prompt)
        ├─ push branch + open PR
        └─▶ redis queue:review
              └─▶ reviewer
                    ├─ review diff (Gemini)
                    ├─ update README if relevant
                    └─ post review comment on PR

[webui] ──┐
          ├─ XRANGE/XREAD  →  run:{id}:events stream        (history + live tail)
          └─ PUBLISH       →  ctrl:{id} (cancel / inject)   (mid-run intervention)
```

All run state lives in Redis:

```
run:{id}                HASH    run metadata (phase, usage, session id, …)
run:{id}:events         STREAM  stream-json events (XADD per event)
runs:index              ZSET    chronological run index
runs:active             SET     currently-running run ids
tokens:daily:{date}     HASH    aggregated token usage per day
ctrl:{id}               PUBSUB  cancel / inject control channel
```

## Quick start

```bash
cp .env.example .env
# fill FORGEJO_URL, FORGEJO_TOKEN, FORGEJO_ISSUE_REPOS, GEMINI_API_KEY

cp projects.json.example projects.json  # (if provided; else create from scratch — see below)
# edit projects.json — one entry per repo you want the orchestrator to implement

just up
# same as: docker compose up --build -d
```

Open http://127.0.0.1:8080 — live dashboard of every run.

Tag any Forgejo issue with label `ai-implement` to trigger the pipeline.

## Commands (justfile)

```bash
just                # list recipes
just up             # docker compose up --build -d (full deploy)
just down           # stop
just restart        # restart without rebuild
just logs           # tail all
just logs webui     # tail one service
just webui-restart  # rebuild + restart a single service
just orch-restart
just reviewer-restart

# local dev (no docker)
just run-webui      # LISTEN_ADDR=127.0.0.1:8080
just run-orch
just run-reviewer
just web-dev        # vite dev server on :5173, proxies /api /ws to :8080

# build + test
just build          # frontend + go
just check          # vet + test
just go-test
just generate       # regen moq mocks

just clean          # wipe node_modules + frontend dist (keeps index.html placeholder)
```

## Configuration

Full list of env vars in [`.env.example`](.env.example).

| Variable                | Required | Default                  | Purpose                                        |
| ----------------------- | -------- | ------------------------ | ---------------------------------------------- |
| `FORGEJO_URL`           | yes      | —                        | Forgejo instance URL                           |
| `FORGEJO_TOKEN`         | yes      | —                        | API token, repo read/write scope               |
| `FORGEJO_ISSUE_REPOS`   | yes (orch) | —                      | `owner/repo` list (comma-separated) to poll    |
| `FORGEJO_ASSIGNEE`      | no       | —                        | Only process issues assigned to this user      |
| `GEMINI_API_KEY`        | yes (reviewer) | —                  | Gemini API key                                 |
| `REDIS_URL`             | no       | `redis://redis:6379`     | Redis connection URL                           |
| `POLL_INTERVAL`         | no       | `60`                     | Seconds between issue polls                    |
| `CLAUDE_TIMEOUT`        | no       | `3600`                   | Per-invocation wall clock limit (seconds)      |
| `GIT_USER_NAME`         | no       | `AI Orchestrator`        | Commit author                                  |
| `GIT_USER_EMAIL`        | no       | `ai@orchestrator.local`  | Commit author email                            |
| `PROJECTS_CONFIG_PATH`  | no       | `./projects.json`        | Host path, mounted into orchestrator           |
| `LISTEN_ADDR`           | no       | `0.0.0.0:8080` (webui)   | Bind address for webui HTTP server             |
| `TZ`                    | no       | `Europe/Rome`            | Container timezone                             |

Claude Code CLI uses subscription auth from `config/claude/.credentials.json` baked into the orchestrator image (see `cmd/orchestrator/Dockerfile`).

## `projects.json`

Maps each repo label to its checkout path and base branch. The orchestrator mounts one volume containing all project checkouts under `/projects` (see `docker-compose.yml`).

```json
{
  "handbook": {
    "repo": "ionstream/handbook",
    "base_branch": "main"
  },
  "api": {
    "repo": "acme/api",
    "base_branch": "main"
  }
}
```

An issue must carry a label `project:<key>` matching one entry above. Example: label `project:handbook` routes the issue into the `handbook` project.

## Forgejo labels

Create these labels on each repo you want the orchestrator to handle:

| Label            | Meaning                                          |
| ---------------- | ------------------------------------------------ |
| `ai-implement`   | Ready for the orchestrator to pick up            |
| `ai-in-progress` | Currently being worked on                        |
| `ai-done`        | PR opened (skipped on future polls)              |
| `ai-needs-info`  | Orchestrator asked clarifying questions, waiting |
| `project:<key>`  | Routes the issue to that project                 |

## Web UI

- `/` — run list (filters by service and phase, 5s poll)
- `/runs/:id` — live run detail via WebSocket: phase badge, token counters, event log (assistant text, tool_use/tool_result, thinking, phase transitions), **Stop** button, **Inject** textarea
- `/tokens` — daily token usage table

REST endpoints (see `cmd/webui/handlers.go`):

```
GET  /api/runs?service=&phase=&limit=&cursor=
GET  /api/runs/active
GET  /api/runs/{id}
GET  /api/runs/{id}/events?since={seq}
POST /api/runs/{id}/cancel
POST /api/runs/{id}/inject   { "note": "..." }
GET  /api/stats/tokens?from=YYYY-MM-DD&to=YYYY-MM-DD
GET  /healthz
GET  /ws/runs/{id}           (WebSocket)
```

### Inject mechanism

Clicking **Inject** publishes a message on `ctrl:{runID}`. The orchestrator cancels the current Claude invocation, drains pending notes, and relaunches with `--resume {sessionID}` — Claude picks up the same conversation with the user's additional instructions prepended.

## Layout

```
cmd/
  orchestrator/     Go binary + Dockerfile (Claude CLI image ~1.5 GB)
  reviewer/         Go binary + Dockerfile (slim Gemini-only image)
  webui/            Go binary + embedded React dist
internal/
  agent/            Claude CLI wrapper (stream-json → events)
  gemini/           Gemini reviewer / docs writer / PR desc writer
  events/           Event types, stream-json parser, Sink interface
  runstore/         Redis Streams + Hash persistence
  control/          Redis PUBSUB cancel/inject channel
  runtime/          Shared main.go boilerplate (signals, redis clients, logger)
  forgejo/          Forgejo API client
  gitops/           git shell wrapper
  queue/            Redis list queue (review jobs)
  testrunner/       run project tests
  models/           shared domain types
web/
  src/              React SPA (TypeScript, Vite)
  Dockerfile        multi-stage: node → vite build → go build → scratch-alpine
```

## Testing

```bash
just go-test
# runs all unit tests with miniredis; no docker required
```

## Roadmap

- [ ] Gemma 3n local reviewer via Ollama
- [ ] Multi-user auth on webui
