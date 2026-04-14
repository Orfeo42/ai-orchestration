FROM python:3.14-slim

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY reviewer/pyproject.toml reviewer/uv.lock* ./
RUN uv sync --frozen --no-dev --no-install-project

COPY shared/ ./shared/
COPY reviewer/ .

ENV PYTHONPATH=/app

CMD ["uv", "run", "python", "main.py"]
