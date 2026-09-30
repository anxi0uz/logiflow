from datetime import UTC, datetime, timedelta
from uuid import uuid4

from dispatch_service.modules.recommendations.schemas import (
    RecommendationRequest,
    RecommendationTrigger,
)


def sample_request() -> RecommendationRequest:
    now = datetime.now(UTC)

    return RecommendationRequest.model_validate(
        {
            "event_id": str(uuid4()),
            "order_id": str(uuid4()),
            "submitted_at": now,
            "requested_at": now,
            "planned_from": now + timedelta(hours=1),
            "planned_to": now + timedelta(hours=3),
            "weight_kg": 100,
            "volume_m3": 2,
            "drivers": [
                {"id": str(uuid4()), "rating": 5},
                {"id": str(uuid4()), "rating": 3},
            ],
            "vehicles": [
                {"id": str(uuid4()), "capacity_kg": 150, "capacity_m3": 3},
                {"id": str(uuid4()), "capacity_kg": 500, "capacity_m3": 10},
                {"id": str(uuid4()), "capacity_kg": 50, "capacity_m3": 1},
            ],
        }
    )


def sample_trigger() -> RecommendationTrigger:
    request = sample_request()
    return RecommendationTrigger.model_validate(
        request.model_dump(
            include={"event_id", "order_id", "submitted_at", "requested_at"}
        )
    )
