FROM python:3.12-slim

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ca-certificates \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g @anthropic-ai/claude-code \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY orchestrator/claude/settings.json /root/.claude/settings.json

RUN git clone --depth 1 https://github.com/JuliusBrussee/caveman.git \
    /root/.claude/plugins/cache/caveman/caveman/92f892f2b997

WORKDIR /app

COPY orchestrator/pyproject.toml orchestrator/uv.lock* ./
RUN uv sync --frozen --no-dev --no-install-project

COPY shared/ ./shared/
COPY orchestrator/ .

ENV PYTHONPATH=/app

CMD ["uv", "run", "python", "main.py"]
