import json
from types import SimpleNamespace

import pytest

from dispatch_service.events import process_message
from dispatch_service.modules.recommendations.service import recommend_candidates
from tests.support import sample_request, sample_trigger

pytestmark = pytest.mark.asyncio


class FakeJS:
    def __init__(self):
        self.published = []

    async def publish(self, subject, data, headers):
        self.published.append((subject, json.loads(data), headers))


class FakeMessage:
    subject = "dispatch.requested.v1"
    metadata = SimpleNamespace(
        stream="LOGIFLOW_EVENTS", sequence=SimpleNamespace(stream=7)
    )

    def __init__(self, data):
        self.data = data
        self.acked = False

    async def ack(self):
        self.acked = True


class FakeService:
    def __init__(self, stale=False):
        self.stale = stale

    async def recommend(self, trigger):
        if self.stale:
            return None
        request = sample_request()
        request.event_id = trigger.event_id
        request.order_id = trigger.order_id
        request.submitted_at = trigger.submitted_at
        request.requested_at = trigger.requested_at
        return recommend_candidates(request)


async def test_valid_request_publishes_before_ack():
    js = FakeJS()
    msg = FakeMessage(sample_trigger().model_dump_json().encode())
    await process_message(msg, js, FakeService())
    assert msg.acked
    assert js.published[0][0] == "dispatch.recommended.v1"
    assert js.published[0][1]["candidates"]
    assert js.published[0][1]["event_id"] == json.loads(msg.data)["event_id"]
    assert js.published[0][2]["Nats-Msg-Id"].startswith("dispatch-result:")


async def test_invalid_request_isolated():
    js = FakeJS()
    msg = FakeMessage(b"{bad")
    await process_message(msg, js, FakeService())
    assert msg.acked
    assert js.published[0][0] == "invalid.events.v1"
    assert js.published[0][1]["payload_base64"] == "e2JhZA=="


async def test_stale_order_is_acknowledged_without_result():
    js = FakeJS()
    msg = FakeMessage(sample_trigger().model_dump_json().encode())
    await process_message(msg, js, FakeService(stale=True))
    assert msg.acked
    assert not js.published
