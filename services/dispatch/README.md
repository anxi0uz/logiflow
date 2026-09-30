# Dispatch

Private Python 3.13 recommendation service. Core publishes `dispatch.requested.v1` with the order ID and submission timestamp after committing the order. Dispatch reads the order and eligible fleet from Core PostgreSQL, ranks driver/vehicle pairs, and publishes `dispatch.recommended.v1`. The same lookup and ranking are available through private gRPC `DispatchRecommendations.Recommend` for an explicit manager refresh. Set `DATABASE_HOST`, `DATABASE_NAME`, `DATABASE_USER`, and `DATABASE_PASSWORD` for `dispatch_reader` (or use `DATABASE_URL`).

Fleet filtering happens in PostgreSQL; at most 30 drivers and 30 vehicles are loaded for ranking. The `dispatch_reader` login can select only the order and fleet tables used by the query; its password is provisioned by the Compose `dispatch-db-role` job, while Goose migration `20260930090000_dispatch_reader.sql` grants table access. Set `LOGIFLOW_DISPATCH_DATABASE_PASSWORD` separately from the Core DB password. The Dispatch connection also uses read-only transactions. Dispatch owns no fleet data or reservations. Core persists the latest result, checks the order state and revalidates all assignment constraints before booking. Stale requests for orders no longer ready for dispatch are skipped.

The package follows the LinkUp/Document Service layout: `core/` contains configuration and logging, `modules/recommendations/` contains SQL lookup, schemas and ranking, and root `events.py`/`grpc_service.py` adapt NATS and gRPC to the same recommendation service.

```bash
uv sync --locked --group dev
uv run --no-sync pytest -q
uv run --no-sync ruff check .
uv run --no-sync ruff format --check .
```
