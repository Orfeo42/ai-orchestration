set shell := ["bash", "-cu"]

web_dir := "web"
dist_dir := "cmd/webui/dist"

default:
    @just --list

# install + build react SPA -> cmd/webui/dist
web:
    cd {{web_dir}} && npm install --no-audit --no-fund
    cd {{web_dir}} && npm run build

# dev server for react SPA (proxies /api, /ws -> 127.0.0.1:8080)
web-dev:
    cd {{web_dir}} && npm install --no-audit --no-fund
    cd {{web_dir}} && npm run dev

# go build all binaries
go-build:
    go build ./...

# go test short
go-test:
    go test -short ./...

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

# build + run ALL services detached (frontend built inside web/Dockerfile)
build-up:
    docker compose up --build -d

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
    docker compose logs -f {{service}}

# rebuild + restart webui only
webui-restart:
    docker compose up --build -d webui

# rebuild + restart orchestrator only
orch-restart:
    docker compose up --build -d orchestrator

# rebuild + restart reviewer only
reviewer-restart:
    docker compose up --build -d reviewer

# run webui locally against local redis
run-webui:
    LISTEN_ADDR=127.0.0.1:8080 REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/webui

# run orchestrator locally
run-orch:
    REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/orchestrator

# run reviewer locally
run-reviewer:
    REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/reviewer

# clean build artifacts (keeps dist/index.html placeholder for go:embed)
clean:
    find {{dist_dir}} -mindepth 1 ! -name index.html -exec rm -rf {} + 2>/dev/null || true
    rm -rf {{web_dir}}/node_modules
