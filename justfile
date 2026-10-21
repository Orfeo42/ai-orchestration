set shell := ["bash", "-cu"]

web_dir := "web"
dist_dir := "cmd/webui/dist"

default:
    @just --list

# install + build react SPA -> cmd/webui/dist
web:
    cd {{ web_dir }} && npm ci --no-audit --no-fund
    cd {{ web_dir }} && npm run build

# dev server for react SPA (proxies /api, /ws -> 127.0.0.1:8080)
web-dev:
    cd {{ web_dir }} && npm install --no-audit --no-fund
    cd {{ web_dir }} && npm run dev

# go build all binaries
go-build:
    go build ./...

# go test short
go-test:
    go test -short ./...

# go tests including integration (needs docker)
go-test-integration pkg="./...":
    go test -count=1 {{ pkg }}

# frontend tests
web-test:
    cd {{ web_dir }} && npm test

# frontend lint
web-lint:
    cd {{ web_dir }} && npm run lint

# go vet
go-vet:
    go vet ./...

# regen moq mocks
generate:
    go generate ./...

# full build: frontend + go
build: web go-build

# full check: vet + test
check: go-vet go-test

# build + run services detached (no arg = all; pass service name to rebuild one)
build-up service="":
    docker compose up --build -d {{ service }}

# docker compose up without rebuild
start:
    docker compose up -d

# docker compose down
down:
    docker compose down

# docker compose restart (no rebuild)
restart:
    docker compose restart

# tail docker logs
logs service="":
    docker compose logs -f {{ service }}

# rebuild + restart webui only
webui-restart:
    docker compose up --build -d webui

# rebuild + restart trigger only
trigger-restart:
    docker compose up --build -d trigger

# rebuild + restart both workers
worker-restart:
    docker compose up --build -d worker-claude worker-default

# scale claude workers to N replicas
scale-claude n="1":
    docker compose up -d --scale worker-claude={{ n }} worker-claude

# run migrations in docker (postgres must be up)
migrate:
    docker compose run --rm --build migrate

# run migrations locally against local postgres
run-migrate:
    DATABASE_URL="${DATABASE_URL:-postgres://orchestrator:${POSTGRES_PASSWORD:-orchestrator}@127.0.0.1:5432/orchestrator?sslmode=disable}" go run ./cmd/migrate

# run webui locally against local redis + postgres
run-webui:
    LISTEN_ADDR=127.0.0.1:8080 DATABASE_URL="${DATABASE_URL:-postgres://orchestrator:${POSTGRES_PASSWORD:-orchestrator}@127.0.0.1:5432/orchestrator?sslmode=disable}" REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/webui

# run trigger locally
run-trigger:
    DATABASE_URL="${DATABASE_URL:-postgres://orchestrator:${POSTGRES_PASSWORD:-orchestrator}@127.0.0.1:5432/orchestrator?sslmode=disable}" WORKFLOWS_DIR="{{ justfile_directory() }}/workflows" PROJECTS_CONFIG="{{ justfile_directory() }}/projects.json" go run ./cmd/trigger

# run worker locally (own Claude config dir, never touches ~/.claude)
run-worker queues="claude,default":
    CLAUDE_CONFIG_DIR="{{ justfile_directory() }}/.cache/claude" CLAUDE_SYSTEM_PROMPT_FILE="{{ justfile_directory() }}/config/claude/headless.md" MEMORY_STATE_DIR="{{ justfile_directory() }}/.cache/memory" PROJECTS_ROOT="{{ justfile_directory() }}/.cache/projects" PROJECTS_CONFIG="{{ justfile_directory() }}/projects.json" DATABASE_URL="${DATABASE_URL:-postgres://orchestrator:${POSTGRES_PASSWORD:-orchestrator}@127.0.0.1:5432/orchestrator?sslmode=disable}" REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/worker --queues={{ queues }}

# clean build artifacts (keeps dist/index.html placeholder for go:embed)
clean:
    find {{ dist_dir }} -mindepth 1 ! -name index.html -exec rm -rf {} + 2>/dev/null || true
    rm -rf {{ web_dir }}/node_modules
