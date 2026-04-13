# Project Plan

## What this is

AI-powered development pipeline. Reads Forgejo issues, implements code autonomously via Claude Code, reviews the result via Gemini or Claude, updates documentation, posts review back to the PR.

---

## Done

### Infrastructure

- [x] Docker Compose with `orchestrator`, `reviewer`, `redis` services
- [x] `uv` for dependency management (`pyproject.toml` per service, no `requirements.txt`)
- [x] Dockerfiles using `uv sync --frozen` for reproducible builds
- [x] `.env` with all required variables documented
- [x] `projects.json` maps project name → Forgejo repo (gitignored, `projects.json.example` committed)
- [x] VS Code workspace (`ai-orchestration.code-workspace`) with Pylance path resolution via `pyrightconfig.json`
- [x] Repos cloned inside container at startup — fully machine-agnostic, no host mounts
- [x] `projects_data` Docker volume shared between `orchestrator` and `reviewer`

### Claude Code config (baked into image)

- [x] `orchestrator/claude/settings.json` — caveman plugin enabled, `effortLevel: medium`
- [x] `orchestrator/claude/CLAUDE.md` — code style + testing rules adapted for container
- [x] Caveman plugin cloned from GitHub during image build (`linuxserver/wireguard` hash pinned)

### Shared (`shared/`)

- [x] Pydantic models: `ForgejoIssue`, `ForgejoPR`, `ReviewJob`, `ReviewResult`
- [x] `ForgejoClient` — Forgejo REST API (issues, labels, comments, PRs, diffs, reviews), optional assignee filter
- [x] `GitOps` — subprocess-based git wrapper (branch, commit, push, fetch, checkout)
- [x] `RedisQueue` — typed `push` / `pop_as[T]` using Pydantic models directly

### Orchestrator (`orchestrator/`)

- [x] Polls `FORGEJO_ISSUE_REPOS` for issues labeled `ai-implement` + optional `FORGEJO_ASSIGNEE` filter
- [x] Issue repo and code repo are separate — code repo resolved via `project:<name>` label on issue
- [x] `RepoManager` — clones or pulls all configured repos on startup
- [x] LangGraph `StateGraph`: `implement → run_tests → create_pr → queue_review`
- [x] Retry loop: test failure re-invokes Claude Code with error context (max 3 attempts)
- [x] `ClaudeAgent` — runs `claude --print` CLI subprocess in project directory
- [x] `TestRunner` — detects Go / pytest / npm and runs tests
- [x] `ProjectConfig` — `repo` + `base_branch`, `path` auto-derived as `/projects/<repo-name>`
- [x] Labels issues `ai-in-progress` / `ai-done`, posts comment with PR link back on issue repo

### Reviewer (`reviewer/`)

- [x] Consumes `queue:review` from Redis
- [x] LangGraph `StateGraph`: `review → update_docs → post_review`
- [x] `ClaudeReviewer` — Anthropic SDK direct, structured output
- [x] `GeminiReviewer` — `gemini-2.0-flash` free tier
- [x] `DocsWriter` — updates `README.md` via Claude, commits to PR branch
- [x] Posts review comment (APPROVED / CHANGES REQUESTED) to Forgejo PR
- [x] Configurable primary reviewer via `PRIMARY_REVIEWER` env var

### Type safety

- [x] No bare `dict`, no `Any`
- [x] Partial state updates typed with `total=False` TypedDicts
- [x] Generic `RedisQueue.pop_as[T]` eliminates untyped deserialization
- [x] `ForgejoPR` Pydantic model — no untyped API response dicts
- [x] `ProjectConfig` validated via `TypeAdapter`

---

## To Do

### Next (required before first real run)

- [x] Generate and commit `uv.lock` for both services
- [ ] End-to-end smoke test against a real Forgejo instance with a dummy issue
- [ ] Verify `claude --print` non-interactive behavior in container (TTY, env, exit codes)
- [ ] Handle Forgejo pagination on `get_issues` (currently capped at 50)

### VPN (FortiVPN)

- [ ] VPN health check before each poll cycle — probe Forgejo URL, skip cycle if unreachable
- [ ] Auto-reconnect via `openfortivpn` CLI if probe fails (credentials via env vars)
- [ ] Backoff strategy on repeated VPN failures (avoid reconnect storm)
- [ ] Note: containers inherit host routing — VPN runs on host, no in-container VPN client needed

### Gemma 4 local (planned next phase)

- [ ] Add `gemma_reviewer.py` — Ollama HTTP client pointing to local instance
- [ ] Add `OLLAMA_URL` env var and `"gemma"` option to `PRIMARY_REVIEWER`
- [ ] Add Ollama service (or use host network) in `docker-compose.yml`
- [ ] Benchmark Gemma vs Gemini on review quality

### Robustness

- [ ] Dead-letter queue for failed review jobs (currently just logged and dropped)
- [ ] Orchestrator: handle git conflicts when branch already exists (e.g. retry after failure)
- [ ] Reviewer: skip README update if PR is documentation-only (avoid no-op commits)
- [ ] Add `FORGEJO_BASE_BRANCH` override per-issue via issue label (e.g. `base:develop`)

### Observability

- [ ] Structured JSON logging (replace `logging.basicConfig`)
- [ ] Expose Redis queue depth as a metric (Prometheus or simple HTTP endpoint)
- [ ] LangGraph trace export (LangSmith or local file) for debugging failed runs

### Security

- [ ] Restrict Claude Code `--allowedTools` per project (config in `projects.json`)
- [ ] Sandbox orchestrator container (no host network, read-only mounts except project dirs)
- [ ] Rotate Forgejo token via env — document token minimum required permissions

---

## Architecture snapshot

```text
Forgejo issue repo (label: ai-implement, project:<name>)
  └─▶ orchestrator
        ├─ resolve code repo from project:<name> label
        ├─ implement  (Claude Code CLI)
        ├─ run tests  → retry ×3 on failure with error context
        ├─ push branch + open PR in code repo
        ├─ comment PR link back on issue
        └─▶ redis queue:review
              └─▶ reviewer
                    ├─ review diff  (Gemini 2.0 Flash / Claude)
                    ├─ update README, commit to PR branch
                    └─ post review comment on PR
```

## Model assignments

| Task                | Model            | Why                                      |
| ------------------- | ---------------- | ---------------------------------------- |
| Code implementation | Claude Code CLI  | full agentic tool use                    |
| PR review           | Gemini 2.0 Flash | free tier, fast, sufficient quality      |
| README update       | Claude SDK       | better long-form writing                 |
| PR review (alt)     | Claude SDK       | switchable via `PRIMARY_REVIEWER=claude` |
| PR review (future)  | Gemma 4 local    | zero cost, privacy, offline              |
