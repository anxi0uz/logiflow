import json

import pytest
from pydantic import ValidationError

from dispatch_service.modules.recommendations.schemas import RecommendationRequest
from dispatch_service.modules.recommendations.service import recommend_candidates
from tests.support import sample_request


def test_recommend_ranks_and_keeps_stable_ids():
    request = sample_request()
    first = recommend_candidates(request)
    second = recommend_candidates(request)
    assert len(first.candidates) == 4
    assert [c.id for c in first.candidates] == [c.id for c in second.candidates]
    assert first.candidates[0].driver_id == request.drivers[0].id
    assert first.candidates[0].vehicle_id == request.vehicles[0].id
    assert all(c.score >= first.candidates[-1].score for c in first.candidates)


def test_invalid_window_is_rejected():
    data = json.loads(sample_request().model_dump_json())
    data["planned_to"] = data["planned_from"]
    with pytest.raises(ValidationError):
        RecommendationRequest.model_validate(data)
