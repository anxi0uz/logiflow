FROM ghcr.io/astral-sh/uv:0.9 AS uv
FROM docker.io/library/python:3.13-slim
COPY --from=uv /uv /uvx /bin/
WORKDIR /app
COPY pyproject.toml uv.lock ./
COPY src/ ./src/
RUN uv sync --locked --no-dev --no-editable
RUN useradd --system --no-create-home --uid 10002 dispatch-service
ENV PATH="/app/.venv/bin:$PATH" PYTHONUNBUFFERED=1
EXPOSE 50052
USER dispatch-service
CMD ["logiflow-dispatch"]
