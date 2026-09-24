# Logiflow Documents

Private Python service for the first document type: a PDF delivery confirmation. It is not an accounting system or an email sender.

After Core confirms delivery, it publishes a `delivery.completed.v1` event containing the data needed for the PDF. This service generates the file, stores it in its own PostgreSQL database, and publishes `document.ready.v1`. Core creates a linked in-app notification. Clients use Core's authenticated HTTP endpoints; the Python gRPC endpoint is private.

| Client endpoint on Core | Result |
| --- | --- |
| `GET /api/v1/documents?limit=20&offset=0` | Current user's inbox |
| `GET /api/v1/documents/{id}/download` | PDF, or 404 for another user's document |

The service owns `documents` and `document_outbox` in its separate database. For this MVP, PDFs are small and stored as PostgreSQL `BYTEA`; object storage can replace that later without changing the external API. `order_id`, `user_id`, driver/vehicle IDs and addresses in the event are an immutable delivery snapshot, not foreign keys into Core's database. Repeated delivery events do not create duplicate documents.

Local dependencies are managed with `uv`:

```bash
cd services/documents
uv sync --group dev
uv run pytest -q
uv run ruff check src tests
uv run ruff format --check src tests
```

The full stack uses `podman-compose.yml` at the repository root. Set `LOGIFLOW_DOCUMENTS_DATABASE_PASSWORD` in the root `.env` outside local development; the compose default is only for local use. The service's required `DATABASE_URL` and `NATS_URL` are supplied by compose. No Python HTTP port or SMTP port is published.
