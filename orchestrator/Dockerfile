FROM python:3.14-slim

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ca-certificates \
    gcc \
    libc6-dev \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g @anthropic-ai/claude-code \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN GO_VERSION=$(curl -fsSL https://go.dev/VERSION?m=text | head -1) \
    && curl -fsSL "https://go.dev/dl/${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xzf -

ENV PATH="/usr/local/go/bin:${PATH}"

RUN useradd -m -s /bin/bash app

COPY orchestrator/claude/settings.json /home/app/.claude/settings.json
COPY orchestrator/claude/CLAUDE.md /home/app/.claude/CLAUDE.md
COPY orchestrator/claude/.credentials.json /home/app/.claude/.credentials.json

RUN git clone --depth 1 https://github.com/JuliusBrussee/caveman.git \
    /home/app/.claude/plugins/cache/caveman/caveman/92f892f2b997 \
    && chown -R app:app /home/app/.claude

WORKDIR /app

COPY orchestrator/pyproject.toml orchestrator/uv.lock* ./
RUN uv sync --frozen --no-dev --no-install-project

COPY shared/ ./shared/
COPY orchestrator/ .

RUN chown -R app:app /app && mkdir -p /projects && chown app:app /projects

ENV PYTHONPATH=/app

USER app

CMD ["uv", "run", "python", "main.py"]
