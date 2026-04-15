# ai-orchestration

AI-powered development pipeline that reads Forgejo issues, implements code autonomously, and reviews the result.

## Services

| Service | Role | Model |
|---|---|---|
| `orchestrator` | Polls issues → implements → opens PR | Claude Code CLI |
| `reviewer` | Reviews PR → updates README → posts comment | Gemini 2.0 Flash / Claude |
| `redis` | Job queue between services | — |

## Flow

```
Forgejo issue (label: ai-implement)
  └─▶ orchestrator
        ├─ implement (Claude Code)
        ├─ run tests → retry up to 3× on failure
        ├─ push branch + open PR
        └─▶ redis queue:review
              └─▶ reviewer
                    ├─ review diff (Gemini or Claude)
                    ├─ update README
                    └─ post review comment on PR
```

## Setup

```bash
cp .env.example .env
# edit .env

cp projects.json.example projects.json
# edit projects.json — map each Forgejo repo to its local path
```

Add volume mounts in `docker-compose.yml` for each project:

```yaml
volumes:
  - /path/to/your/repo:/projects/repo-name
```

Create labels on each Forgejo repo: `ai-implement`, `ai-in-progress`, `ai-done`.

```bash
docker compose up --build
```

Tag any issue with `ai-implement` to trigger the pipeline.

## Configuration

| Variable | Description |
|---|---|
| `FORGEJO_URL` | Forgejo instance URL |
| `FORGEJO_TOKEN` | API token with repo read/write |
| `FORGEJO_REPOS` | Comma-separated `owner/repo` list |
| `ANTHROPIC_API_KEY` | For orchestrator + reviewer (Claude) |
| `GEMINI_API_KEY` | For reviewer (Gemini) |
| `PRIMARY_REVIEWER` | `gemini` (default) or `claude` |
| `POLL_INTERVAL` | Seconds between issue polls (default: 60) |
| `PROJECTS_CONFIG_PATH` | Path to `projects.json` (default: `./projects.json`) |

## Projects config

```json
{
  "owner/repo": {
    "path": "/projects/repo",
    "base_branch": "main"
  }
}
```

## Roadmap

- [ ] Gemma 4 local reviewer via Ollama
