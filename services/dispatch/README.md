# Dispatch

Private Python 3.13 recommendation service. Core publishes `dispatch.requested.v1` with an order and best-effort fleet snapshot. Dispatch ranks driver/vehicle pairs and publishes `dispatch.recommended.v1`. The same ranking is available through private gRPC `DispatchRecommendations.Recommend` for an explicit manager refresh.

Dispatch owns no fleet data or reservations. Core persists the latest result, checks the order state and revalidates all assignment constraints before booking.

```bash
uv sync --locked --group dev
uv run --no-sync pytest -q
uv run --no-sync ruff check .
uv run --no-sync ruff format --check .
```
