FROM ghcr.io/astral-sh/uv:0.9 AS uv

FROM docker.io/library/python:3.13-slim

COPY --from=uv /uv /uvx /bin/
RUN apt-get update && apt-get install -y --no-install-recommends fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY pyproject.toml uv.lock ./
COPY src/ ./src/
COPY alembic.ini ./
RUN uv sync --locked --no-dev --no-editable
RUN useradd --system --no-create-home --uid 10001 document-service

ENV PATH="/app/.venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    FONT_PATH=/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf

EXPOSE 50051
USER document-service
CMD ["sh", "-c", "alembic upgrade head && exec logiflow-documents"]
