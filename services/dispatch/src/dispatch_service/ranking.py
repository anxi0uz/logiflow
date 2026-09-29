from datetime import UTC, datetime
from uuid import uuid5

from dispatch_service.schemas import Candidate, Recommended, Request


def recommend(request: Request) -> Recommended:
    candidates = []
    for driver in request.drivers:
        for vehicle in request.vehicles:
            if (
                vehicle.capacity_kg < request.weight_kg
                or vehicle.capacity_m3 < request.volume_m3
            ):
                continue
            # Prefer a well-rated driver and a suitably sized vehicle.
            spare_kg = (vehicle.capacity_kg - request.weight_kg) / max(
                vehicle.capacity_kg, 1
            )
            spare_m3 = (vehicle.capacity_m3 - request.volume_m3) / max(
                vehicle.capacity_m3, 1
            )
            score = round(driver.rating * 20 + (1 - (spare_kg + spare_m3) / 2) * 10, 2)
            candidates.append(
                Candidate(
                    id=str(uuid5(request.event_id, f"{driver.id}:{vehicle.id}")),
                    driver_id=driver.id,
                    vehicle_id=vehicle.id,
                    score=score,
                    reason="Рейтинг водителя и соответствие вместимости груза",
                )
            )
    candidates.sort(key=lambda c: (-c.score, str(c.driver_id), str(c.vehicle_id)))
    return Recommended(
        event_id=request.event_id,
        order_id=request.order_id,
        submitted_at=request.submitted_at,
        requested_at=request.requested_at,
        created_at=datetime.now(UTC),
        candidates=candidates[:5],
    )
