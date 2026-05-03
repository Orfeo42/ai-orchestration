set shell := ["bash", "-cu"]

web_dir := "web"
dist_dir := "cmd/webui/dist"

default:
    @just --list

# install + build react SPA -> cmd/webui/dist
web:
    cd {{ web_dir }} && npm install --no-audit --no-fund
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

# rebuild + restart producer only
producer-restart:
    docker compose up --build -d producer

# rebuild + restart every consumer
consumer-restart:
    docker compose up --build -d assess-consumer impl-consumer review-consumer

# scale impl-consumer to N replicas
scale-impl n="1":
    docker compose up -d --scale impl-consumer={{ n }} impl-consumer

# run webui locally against local redis
run-webui:
    LISTEN_ADDR=127.0.0.1:8080 REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/webui

# run producer locally
run-producer:
    REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/producer

# run assess consumer locally
run-assess-consumer:
    REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/consumer --pipeline=assess

# run impl consumer locally
run-impl-consumer:
    REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/consumer --pipeline=impl

# run review consumer locally
run-review-consumer:
    REDIS_URL="${REDIS_URL:-redis://127.0.0.1:6379}" go run ./cmd/consumer --pipeline=review

# clean build artifacts (keeps dist/index.html placeholder for go:embed)
clean:
    find {{ dist_dir }} -mindepth 1 ! -name index.html -exec rm -rf {} + 2>/dev/null || true
    rm -rf {{ web_dir }}/node_modules

update-credential:
    cp ~/.claude/.credentials.json ./config/claude/.credentials.json
