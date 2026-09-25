import asyncio
import json
from datetime import UTC, datetime
from types import SimpleNamespace
from unittest.mock import AsyncMock
from uuid import UUID, uuid4

import pytest

from document_service import events
from document_service.models import Document, OutboxEvent
from document_service.modules.documents import service


class Message:
    def __init__(self, data):
        self.data = data
        self.subject = "delivery.completed.v1"
        self.metadata = SimpleNamespace(
            stream="LOGIFLOW_EVENTS", sequence=SimpleNamespace(stream=42)
        )
        self.ack = AsyncMock()
        self.nak = AsyncMock()


class Sessions:
    def __call__(self):
        return self

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_):
        pass


def valid_payload():
    return {
        "event_id": str(uuid4()),
        "order_id": str(uuid4()),
        "user_id": str(uuid4()),
        "origin_address": "Origin",
        "destination_address": "Destination",
        "cargo_description": "Cargo",
        "weight_kg": 1,
        "volume_m3": 1,
        "delivered_at": datetime.now(UTC).isoformat(),
        "driver_id": str(uuid4()),
        "vehicle_id": str(uuid4()),
    }


@pytest.mark.parametrize(
    "data",
    [
        b"{broken",
        json.dumps({**valid_payload(), "order_id": str(UUID(int=0))}).encode(),
    ],
)
def test_invalid_delivery_is_isolated_before_ack(data):
    async def scenario():
        message = Message(data)
        js = SimpleNamespace(publish=AsyncMock())
        js.publish.side_effect = ConnectionError("NATS unavailable")
        with pytest.raises(ConnectionError):
            await events.process_delivery_message(message, js, Sessions(), "font")
        message.ack.assert_not_awaited()

        js.publish.side_effect = None
        await events.process_delivery_message(message, js, Sessions(), "font")
        diagnostic = json.loads(js.publish.await_args.args[1])
        assert diagnostic["source_sequence"] == 42
        assert diagnostic["payload_base64"]
        assert js.publish.await_args.kwargs["headers"]["Nats-Msg-Id"].endswith(
            ":42:document-generator"
        )
        message.ack.assert_awaited_once()

    asyncio.run(scenario())


def test_transient_delivery_failure_then_duplicate(monkeypatch):
    async def scenario():
        data = json.dumps(valid_payload()).encode()
        message = Message(data)
        js = SimpleNamespace(publish=AsyncMock())
        monkeypatch.setattr(
            service, "render_delivery_confirmation", lambda *_: b"%PDF-test"
        )

        class Database(Sessions):
            def __init__(self):
                self.document = None
                self.pending = []
                self.outbox_count = 0

            async def scalar(self, _query):
                return self.document

            def add(self, value):
                self.pending.append(value)

            async def flush(self):
                for value in self.pending:
                    if isinstance(value, Document) and value.id is None:
                        value.id = uuid4()

            async def commit(self):
                for value in self.pending:
                    if isinstance(value, Document):
                        self.document = value
                    elif isinstance(value, OutboxEvent):
                        self.outbox_count += 1
                self.pending.clear()

        db = Database()
        attempts = 0

        async def accept(db, event, font):
            nonlocal attempts
            attempts += 1
            if attempts == 1:
                raise ConnectionError("database unavailable")
            return await service.accept_delivery(db, event, font)

        monkeypatch.setattr(events, "accept_delivery", accept)
        with pytest.raises(ConnectionError):
            await events.process_delivery_message(message, js, Sessions(), "font")
        message.ack.assert_not_awaited()
        await events.process_delivery_message(message, js, db, "font")
        await events.process_delivery_message(message, js, db, "font")
        assert db.document is not None
        assert db.outbox_count == 1
        assert attempts == 3
        assert message.ack.await_count == 2
        js.publish.assert_not_awaited()

    asyncio.run(scenario())
