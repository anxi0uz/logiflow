# Logiflow Documents

Private Python service for six internal PDF types. These are operational records for an educational logistics system, not legally signed Russian electronic transport documents, waybills, or accounting originals.

Core publishes immutable snapshots in `document.requested.v1`; delivery confirmation retains `delivery.completed.v1`. This service generates the file, stores it in its own PostgreSQL database, and publishes `document.ready.v1` for each recipient. Core creates linked in-app notifications. Clients use Core's authenticated HTTP endpoints; the Python gRPC endpoint is private.

New order snapshots include sender, recipient and carrier details, transport basis and declared package count. Final documents also include the actual delivery receipt: recipient role, stated authority, actual quantity, condition, seal, remarks and recording metadata. Optional `fields` on `delivery.completed.v1` preserves compatibility with older events. Long values continue across pages, including text without spaces. Counterparty edits never rewrite issued PDFs.

| Type | Trigger | Reference |
| --- | --- | --- |
| `transport_order` | Order submitted for dispatch | Order |
| `trip_assignment` | Trip started or replacement assignment activated | Assignment |
| `cargo_handover` | Both handover confirmations completed | Handover |
| `delivery_confirmation` | Delivery completed | Order |
| `service_report` | Delivery completed, with assignment history | Order |
| `cancellation_notice` | Order cancelled before departure | Order |

| Client endpoint on Core | Result |
| --- | --- |
| `GET /api/v1/documents?limit=20&offset=0` | Current user's inbox |
| `GET /api/v1/documents/{id}/download` | PDF, or 404 without ownership/recipient membership |

The service owns `documents`, `document_recipients` and `document_outbox` in its separate database. PDFs are stored as PostgreSQL `BYTEA`. Core identifiers and snapshot fields are not foreign keys into Core's database. The unique `(reference_id, document_type)` key makes retries idempotent while allowing multiple handovers/assignments per order. Once issued, PDF bytes are reused; profile edits do not rewrite the file. Legacy documents are backfilled with `reference_id=order_id`.

Generic document recipients include the owner and relevant actors/assignment participants; handover protocols also include the outgoing driver. Existing delivery confirmations retain owner-only access. An unrelated manager has no global document access. Authentication and recipient selection belong to Core; the private gRPC port must not be exposed publicly.

Malformed inter-service events are copied to the private JetStream stream `LOGIFLOW_INVALID_EVENTS` on subject `invalid.events.v1` before their source messages are acknowledged. Each diagnostic JSON record includes the source stream, sequence and subject, consumer, validation reason, and original bytes as `payload_base64`. Inspect the stream with JetStream tooling and use the source sequence to locate an event; restrict access because delivery payloads may contain customer data. Database or NATS failures keep the source message eligible for retry.

Local dependencies are managed with `uv`:

```bash
cd services/documents
uv sync --group dev
uv run pytest -q
uv run ruff check src tests
uv run ruff format --check src tests
```

PDF tests need DejaVu Sans: install it in a standard system path or set `FONT_PATH` to the TTF file. The container and CI include this dependency. Tests cover Cyrillic text, escaped input, multipage rendering, recipient deduplication and immutable replay.

The full stack uses `podman-compose.yml` at the repository root. Set `LOGIFLOW_DOCUMENTS_DATABASE_PASSWORD` in the root `.env` outside local development; the compose default is only for local use. The service's required `DATABASE_URL` and `NATS_URL` are supplied by compose. No Python HTTP port or SMTP port is published.
