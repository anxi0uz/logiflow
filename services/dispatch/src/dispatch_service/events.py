import asyncio
import base64
import json

import structlog
from pydantic import ValidationError

from dispatch_service.ranking import recommend
from dispatch_service.schemas import Request

log = structlog.get_logger(component="dispatch.events")


async def process_message(message, js):
    try:
        request = Request.model_validate_json(message.data)
    except ValidationError as exc:
        metadata = message.metadata
        diagnostic = {
            "source_stream": metadata.stream,
            "source_sequence": metadata.sequence.stream,
            "source_subject": message.subject,
            "consumer": "dispatch-recommender",
            "reason": json.dumps(exc.errors(include_input=False, include_url=False)),
            "payload_base64": base64.b64encode(message.data).decode(),
        }
        await js.publish(
            "invalid.events.v1",
            json.dumps(diagnostic).encode(),
            headers={
                "Nats-Msg-Id": f"invalid:{metadata.stream}:{metadata.sequence.stream}:dispatch-recommender"
            },
        )
        log.error("invalid_dispatch_request", reason=diagnostic["reason"])
        await message.ack()
        return
    result = recommend(request)
    await js.publish(
        "dispatch.recommended.v1",
        result.model_dump_json().encode(),
        headers={"Nats-Msg-Id": f"dispatch-result:{request.event_id}"},
    )
    await message.ack()


async def consume_requests(js):
    subscription = await js.pull_subscribe(
        "dispatch.requested.v1",
        durable="dispatch-recommender",
        stream="LOGIFLOW_EVENTS",
    )
    while True:
        try:
            messages = await subscription.fetch(batch=10, timeout=1)
        except TimeoutError:
            continue
        except Exception:
            log.exception("dispatch_subscription_failed")
            await asyncio.sleep(2)
            continue
        for message in messages:
            try:
                await process_message(message, js)
            except Exception:
                log.exception("dispatch_recommendation_failed")
                await message.nak(delay=5)
