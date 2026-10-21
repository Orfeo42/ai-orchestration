FROM golang:1.27.3-trixie AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /worker ./cmd/worker

FROM golang:1.27.3-trixie
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ca-certificates \
    && curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g @anthropic-ai/claude-code \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash app

COPY config/claude/settings.json /home/app/.claude/settings.json
COPY config/claude/headless.md /opt/orchestrator/claude/headless.md

RUN git clone --depth 1 https://github.com/JuliusBrussee/caveman.git \
    /home/app/.claude/plugins/cache/caveman/caveman/92f892f2b997 \
    && chown -R app:app /home/app/.claude

RUN mkdir -p /projects /state/memory && chown -R app:app /projects /state

COPY --from=builder /worker /usr/local/bin/worker

USER app
ENTRYPOINT ["/usr/local/bin/worker"]
