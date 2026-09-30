from datetime import UTC, datetime
from uuid import uuid5

import structlog

from dispatch_service.modules.recommendations.repository import FleetRepository
from dispatch_service.modules.recommendations.schemas import (
    RecommendationCandidate,
    RecommendationRequest,
    RecommendationResult,
    RecommendationTrigger,
)

log = structlog.get_logger(component="recommendations.service")


class RecommendationService:
    def __init__(self, repository: FleetRepository) -> None:
        self.repository = repository

    async def recommend(
        self, trigger: RecommendationTrigger
    ) -> RecommendationResult | None:
        request = await self.repository.load_request(trigger)
        if request is None:
            return None
        return recommend_candidates(request)


def recommend_candidates(
    request: RecommendationRequest,
) -> RecommendationResult:
    candidates: list[RecommendationCandidate] = []
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
                RecommendationCandidate(
                    id=str(uuid5(request.event_id, f"{driver.id}:{vehicle.id}")),
                    driver_id=driver.id,
                    vehicle_id=vehicle.id,
                    score=score,
                    reason="Рейтинг водителя и соответствие вместимости груза",
                )
            )
    candidates.sort(key=lambda c: (-c.score, str(c.driver_id), str(c.vehicle_id)))
    result = RecommendationResult(
        event_id=request.event_id,
        order_id=request.order_id,
        submitted_at=request.submitted_at,
        requested_at=request.requested_at,
        created_at=datetime.now(UTC),
        candidates=candidates[:5],
    )

    log.info(
        "recommendations_created",
        order_id=str(request.order_id),
        driver_count=len(request.drivers),
        vehicle_count=len(request.vehicles),
        candidate_count=len(result.candidates),
    )

    return result
