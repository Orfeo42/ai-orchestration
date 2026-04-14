FROM orchestrator-base:latest

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
