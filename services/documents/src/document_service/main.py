import asyncio

import grpc
import nats
import structlog
from nats.js.errors import APIError, NotFoundError

from document_service.core.config import get_settings
from document_service.db.session import engine, session_factory
from document_service.events import consume_deliveries, publish_ready_events
from document_service.grpc_inbox import DocumentInbox
from logiflow.documents.v1 import documents_pb2_grpc

log = structlog.get_logger(component="documents.main")


async def run() -> None:
    settings = get_settings()
    nc = await nats.connect(settings.nats_url)
    js = nc.jetstream()
    try:
        await js.stream_info("LOGIFLOW_EVENTS")
    except NotFoundError:
        try:
            await js.add_stream(
                name="LOGIFLOW_EVENTS",
                subjects=["delivery.completed.v1", "document.ready.v1"],
            )
        except APIError:
            # Core may have created the stream at the same instant.
            await js.stream_info("LOGIFLOW_EVENTS")

    server = grpc.aio.server()
    documents_pb2_grpc.add_DocumentInboxServicer_to_server(
        DocumentInbox(session_factory), server
    )
    server.add_insecure_port(f"{settings.grpc_host}:{settings.grpc_port}")
    await server.start()
    log.info("document_service_started", grpc_port=settings.grpc_port)

    tasks = [
        asyncio.create_task(
            consume_deliveries(js, session_factory, settings.font_path)
        ),
        asyncio.create_task(publish_ready_events(js, session_factory)),
    ]
    try:
        await server.wait_for_termination()
    finally:
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
        await server.stop(grace=5)
        await nc.drain()
        await engine.dispose()


def main() -> None:
    asyncio.run(run())


if __name__ == "__main__":
    main()
