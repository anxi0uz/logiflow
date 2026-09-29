import asyncio
import json
from types import SimpleNamespace

from test_ranking import sample_request

from dispatch_service.events import process_message


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


def test_valid_request_publishes_before_ack():
    js = FakeJS()
    msg = FakeMessage(sample_request().model_dump_json().encode())
    asyncio.run(process_message(msg, js))
    assert msg.acked
    assert js.published[0][0] == "dispatch.recommended.v1"
    assert js.published[0][1]["candidates"]
    assert js.published[0][2]["Nats-Msg-Id"].startswith("dispatch-result:")


def test_invalid_request_isolated():
    js = FakeJS()
    msg = FakeMessage(b"{bad")
    asyncio.run(process_message(msg, js))
    assert msg.acked
    assert js.published[0][0] == "invalid.events.v1"
    assert js.published[0][1]["payload_base64"] == "e2JhZA=="
