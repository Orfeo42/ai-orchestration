FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod ./
RUN go mod tidy
COPY . .
RUN CGO_ENABLED=0 go build -o /consumer ./cmd/consumer

FROM golang:1.26-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ca-certificates \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g @anthropic-ai/claude-code \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash app

COPY config/claude/settings.json /home/app/.claude/settings.json
COPY config/claude/CLAUDE.md /home/app/.claude/CLAUDE.md
COPY config/claude/.credentials.json /home/app/.claude/.credentials.json
COPY config/claude/skills /home/app/.claude/skills

RUN git clone --depth 1 https://github.com/JuliusBrussee/caveman.git \
    /home/app/.claude/plugins/cache/caveman/caveman/92f892f2b997 \
    && chown -R app:app /home/app/.claude

RUN mkdir -p /projects && chown app:app /projects

COPY --from=builder /consumer /usr/local/bin/consumer

USER app
ENTRYPOINT ["/usr/local/bin/consumer"]
