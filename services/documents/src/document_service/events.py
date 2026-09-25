import asyncio
import base64
import json
from datetime import UTC, datetime

import structlog
from nats.js.client import JetStreamContext
from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from document_service.models import OutboxEvent
from document_service.modules.documents.schemas import DeliveryCompleted
from document_service.modules.documents.service import accept_delivery

log = structlog.get_logger(component="documents.events")


async def process_delivery_message(message, js, sessions, font_path):
    try:
        event = DeliveryCompleted.model_validate_json(message.data)
    except ValidationError as exc:
        metadata = message.metadata
        diagnostic = {
            "source_stream": metadata.stream,
            "source_sequence": metadata.sequence.stream,
            "source_subject": message.subject,
            "consumer": "document-generator",
            "reason": json.dumps(
                exc.errors(include_input=False, include_url=False), default=str
            ),
            "payload_base64": base64.b64encode(message.data).decode("ascii"),
        }
        await js.publish(
            "invalid.events.v1",
            json.dumps(diagnostic).encode(),
            headers={
                "Nats-Msg-Id": f"invalid:{metadata.stream}:{metadata.sequence.stream}:document-generator"
            },
        )
        log.error(
            "invalid_delivery_event_isolated",
            **diagnostic | {"payload_base64": "[redacted]"},
        )
        await message.ack()
        return

    async with sessions() as db:
        await accept_delivery(db, event, font_path)
    await message.ack()


async def consume_deliveries(
    js: JetStreamContext,
    sessions: async_sessionmaker[AsyncSession],
    font_path: str,
) -> None:
    subscription = await js.pull_subscribe(
        "delivery.completed.v1", durable="document-generator", stream="LOGIFLOW_EVENTS"
    )
    while True:
        try:
            messages = await subscription.fetch(batch=10, timeout=1)
        except TimeoutError:
            continue
        except Exception:
            log.exception("delivery_subscription_failed")
            await asyncio.sleep(2)
            continue
        for message in messages:
            try:
                await process_delivery_message(message, js, sessions, font_path)
            except Exception:
                log.exception("delivery_document_failed")
                await message.nak(delay=5)


async def publish_ready_events(
    js: JetStreamContext,
    sessions: async_sessionmaker[AsyncSession],
) -> None:
    while True:
        try:
            async with sessions() as db, db.begin():
                rows = await db.scalars(
                    select(OutboxEvent)
                    .where(OutboxEvent.published_at.is_(None))
                    .order_by(OutboxEvent.created_at, OutboxEvent.id)
                    .limit(20)
                    .with_for_update(skip_locked=True)
                )
                for event in rows:
                    await js.publish(
                        event.subject,
                        json.dumps(event.payload).encode(),
                        headers={"Nats-Msg-Id": str(event.id)},
                    )
                    event.published_at = datetime.now(UTC)
        except Exception:
            log.exception("document_outbox_publish_failed")
        await asyncio.sleep(1)
