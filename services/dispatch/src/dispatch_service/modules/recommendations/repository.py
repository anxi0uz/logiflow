import asyncpg

from dispatch_service.modules.recommendations.schemas import (
    RecommendationRequest,
    RecommendationTrigger,
)

ORDER_SQL = """
SELECT submitted_at, pickup_from, pickup_to, weight_kg, volume_m3
FROM orders
WHERE id = $1 AND status = 'ready_for_dispatch'
"""

DRIVERS_SQL = """
SELECT d.id, LEAST(GREATEST(COALESCE(d.rating, 0), 0), 5) AS rating
FROM drivers d
WHERE d.status = 'available'
  AND (EXISTS (
    SELECT 1 FROM driver_documents dd
    WHERE dd.driver_id = d.id AND dd.type = 'license'
      AND dd.status = 'valid' AND dd.valid_until >= $2::timestamptz::date
  ) OR (NOT EXISTS (
    SELECT 1 FROM driver_documents dd
    WHERE dd.driver_id = d.id AND dd.type = 'license'
  ) AND d.license_expiry >= $2::timestamptz::date))
  AND (NOT EXISTS (SELECT 1 FROM driver_shifts ds WHERE ds.driver_id = d.id)
    OR EXISTS (
      SELECT 1 FROM driver_shifts ds WHERE ds.driver_id = d.id
        AND ds.starts_at <= $1 AND ds.ends_at >= $2
    ))
  AND NOT EXISTS (
    SELECT 1 FROM assignments a
    WHERE a.driver_id = d.id
      AND a.status IN ('pending_acceptance', 'accepted', 'active')
      AND tstzrange(a.planned_from, a.planned_to, '[)')
          && tstzrange($1, $2, '[)')
  )
ORDER BY d.rating DESC, d.id
LIMIT 30
"""

VEHICLES_SQL = """
SELECT v.id, v.capacity_kg, v.capacity_m3
FROM vehicles v
WHERE v.status = 'available'
  AND v.capacity_kg >= $3 AND v.capacity_m3 >= $4
  AND EXISTS (
    SELECT 1 FROM vehicle_documents vd
    WHERE vd.vehicle_id = v.id AND vd.type = 'registration'
      AND vd.status = 'valid' AND vd.valid_until >= $2::timestamptz::date
  )
  AND NOT EXISTS (
    SELECT 1 FROM assignments a
    WHERE a.vehicle_id = v.id
      AND a.status IN ('pending_acceptance', 'accepted', 'active')
      AND tstzrange(a.planned_from, a.planned_to, '[)')
          && tstzrange($1, $2, '[)')
  )
ORDER BY v.capacity_kg, v.id
LIMIT 30
"""


class FleetRepository:
    def __init__(self, pool: asyncpg.Pool) -> None:
        self.pool = pool

    async def load_request(
        self, trigger: RecommendationTrigger
    ) -> RecommendationRequest | None:
        async with (
            self.pool.acquire() as conn,
            conn.transaction(isolation="repeatable_read", readonly=True),
        ):
            order = await conn.fetchrow(ORDER_SQL, trigger.order_id)
            if order is None or order["submitted_at"] != trigger.submitted_at:
                return None
            planned_from = order["pickup_from"]
            planned_to = order["pickup_to"]
            drivers = await conn.fetch(DRIVERS_SQL, planned_from, planned_to)
            vehicles = await conn.fetch(
                VEHICLES_SQL,
                planned_from,
                planned_to,
                order["weight_kg"],
                order["volume_m3"],
            )
        return RecommendationRequest.model_validate(
            {
                "event_id": trigger.event_id,
                "order_id": trigger.order_id,
                "submitted_at": trigger.submitted_at,
                "requested_at": trigger.requested_at,
                "planned_from": planned_from,
                "planned_to": planned_to,
                "weight_kg": order["weight_kg"],
                "volume_m3": order["volume_m3"],
                "drivers": [dict(row) for row in drivers],
                "vehicles": [dict(row) for row in vehicles],
            }
        )
