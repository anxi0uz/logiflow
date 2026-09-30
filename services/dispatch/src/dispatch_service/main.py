import asyncio
import signal

import asyncpg
import grpc
import nats
import structlog
from nats.js.client import JetStreamContext
from nats.js.errors import APIError, NotFoundError

from dispatch_service.core.config import get_settings
from dispatch_service.core.logger import configure_logging
from dispatch_service.events import consume_requests
from dispatch_service.grpc_service import DispatchRecommendations
from dispatch_service.modules.recommendations.repository import FleetRepository
from dispatch_service.modules.recommendations.service import RecommendationService
from logiflow.dispatch.v1 import dispatch_pb2_grpc

log = structlog.get_logger(component="dispatch.main")


async def ensure_stream(
    js: JetStreamContext,
    name: str,
    subjects: list[str],
) -> None:
    try:
        info = await js.stream_info(name)
    except NotFoundError:
        try:
            await js.add_stream(name=name, subjects=subjects)
        except APIError:
            info = await js.stream_info(name)
        else:
            return
    current = set(info.config.subjects)
    if not set(subjects).issubset(current):
        config = info.config
        config.subjects = sorted(current | set(subjects))
        await js.update_stream(config)


async def run() -> None:
    settings = get_settings()
    configure_logging(settings.debug)

    pool = await asyncpg.create_pool(
        settings.database_url,
        host=None if settings.database_url else settings.database_host,
        port=None if settings.database_url else settings.database_port,
        database=None if settings.database_url else settings.database_name,
        user=None if settings.database_url else settings.database_user,
        password=None if settings.database_url else settings.database_password,
        min_size=1,
        max_size=5,
        server_settings={"default_transaction_read_only": "on"},
    )
    service = RecommendationService(FleetRepository(pool))
    nc = await nats.connect(settings.nats_url)
    js = nc.jetstream()
    await ensure_stream(
        js,
        "LOGIFLOW_EVENTS",
        [
            "delivery.completed.v1",
            "document.ready.v1",
            "dispatch.requested.v1",
            "dispatch.recommended.v1",
        ],
    )
    await ensure_stream(js, "LOGIFLOW_INVALID_EVENTS", ["invalid.events.v1"])
    server = grpc.aio.server()
    dispatch_pb2_grpc.add_DispatchRecommendationsServicer_to_server(
        DispatchRecommendations(service), server
    )
    server.add_insecure_port(f"{settings.grpc_host}:{settings.grpc_port}")
    await server.start()
    log.info("dispatch_service_started", grpc_port=settings.grpc_port)

    worker = asyncio.create_task(
        consume_requests(js, service), name="consume_dispatch_requests"
    )
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, stop.set)
    stop_task = asyncio.create_task(stop.wait())
    server_task = asyncio.create_task(server.wait_for_termination())
    try:
        done, _ = await asyncio.wait(
            [worker, stop_task, server_task],
            return_when=asyncio.FIRST_COMPLETED,
        )
        if worker in done:
            worker.result()
            raise RuntimeError("dispatch consumer stopped unexpectedly")
        if server_task in done:
            server_task.result()
            raise RuntimeError("dispatch gRPC server stopped unexpectedly")
    finally:
        worker.cancel()
        stop_task.cancel()
        await asyncio.gather(worker, stop_task, return_exceptions=True)
        await server.stop(grace=3)
        await server_task
        await pool.close()
        try:
            await asyncio.wait_for(nc.drain(), timeout=3)
        except Exception:
            log.warning("nats_drain_failed_during_shutdown", exc_info=True)
            try:
                await asyncio.wait_for(nc.close(), timeout=1)
            except Exception:
                log.warning("nats_close_failed_during_shutdown", exc_info=True)
        log.info("dispatch_service_stopped")


def main() -> None:
    asyncio.run(run())


if __name__ == "__main__":
    main()
