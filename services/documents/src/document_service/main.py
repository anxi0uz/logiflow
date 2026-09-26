import asyncio
import signal

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
    try:
        await js.stream_info("LOGIFLOW_INVALID_EVENTS")
    except NotFoundError:
        try:
            await js.add_stream(
                name="LOGIFLOW_INVALID_EVENTS", subjects=["invalid.events.v1"]
            )
        except APIError:
            await js.stream_info("LOGIFLOW_INVALID_EVENTS")

    server = grpc.aio.server()
    documents_pb2_grpc.add_DocumentInboxServicer_to_server(
        DocumentInbox(session_factory), server
    )
    server.add_insecure_port(f"{settings.grpc_host}:{settings.grpc_port}")
    await server.start()
    log.info("document_service_started", grpc_port=settings.grpc_port)

    workers = [
        asyncio.create_task(
            consume_deliveries(js, session_factory, settings.font_path),
            name="consume_deliveries",
        ),
        asyncio.create_task(
            publish_ready_events(js, session_factory), name="publish_ready_events"
        ),
    ]
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for shutdown_signal in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(shutdown_signal, stop.set)
    stop_task = asyncio.create_task(stop.wait())
    server_task = asyncio.create_task(server.wait_for_termination())
    try:
        done, _ = await asyncio.wait(
            [stop_task, server_task, *workers], return_when=asyncio.FIRST_COMPLETED
        )
        for worker in workers:
            if worker in done:
                worker.result()
                raise RuntimeError(f"{worker.get_name()} stopped unexpectedly")
    finally:
        for task in [stop_task, *workers]:
            task.cancel()
        await asyncio.gather(stop_task, *workers, return_exceptions=True)
        await server.stop(grace=3)
        await server_task
        try:
            await asyncio.wait_for(nc.drain(), timeout=3)
        except Exception:
            log.warning("nats_drain_failed_during_shutdown", exc_info=True)
            try:
                await asyncio.wait_for(nc.close(), timeout=1)
            except Exception:
                log.warning("nats_close_failed_during_shutdown", exc_info=True)
        await engine.dispose()


def main() -> None:
    asyncio.run(run())


if __name__ == "__main__":
    main()
