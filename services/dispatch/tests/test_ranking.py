import json
from datetime import UTC, datetime, timedelta
from uuid import uuid4

import pytest
from pydantic import ValidationError

from dispatch_service.ranking import recommend
from dispatch_service.schemas import Request


def sample_request():
    now = datetime.now(UTC)
    return Request.model_validate(
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


def test_recommend_ranks_and_keeps_stable_ids():
    request = sample_request()
    first = recommend(request)
    second = recommend(request)
    assert len(first.candidates) == 4
    assert [c.id for c in first.candidates] == [c.id for c in second.candidates]
    assert first.candidates[0].driver_id == request.drivers[0].id
    assert first.candidates[0].vehicle_id == request.vehicles[0].id
    assert all(c.score >= first.candidates[-1].score for c in first.candidates)


def test_invalid_window_is_rejected():
    data = json.loads(sample_request().model_dump_json())
    data["planned_to"] = data["planned_from"]
    with pytest.raises(ValidationError):
        Request.model_validate(data)
