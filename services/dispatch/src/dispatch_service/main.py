import asyncio
import os
import signal

import grpc
import nats
import structlog
from nats.js.errors import APIError, NotFoundError

from dispatch_service.events import consume_requests
from dispatch_service.grpc_service import DispatchRecommendations
from logiflow.dispatch.v1 import dispatch_pb2_grpc

log = structlog.get_logger(component="dispatch.main")


async def ensure_stream(js, name, subjects):
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


async def run():
    nc = await nats.connect(os.getenv("NATS_URL", "nats://127.0.0.1:4222"))
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
        DispatchRecommendations(), server
    )
    port = int(os.getenv("GRPC_PORT", "50052"))
    server.add_insecure_port(f"0.0.0.0:{port}")
    await server.start()
    worker = asyncio.create_task(consume_requests(js), name="consume_dispatch_requests")
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, stop.set)
    stop_task = asyncio.create_task(stop.wait())
    try:
        done, _ = await asyncio.wait(
            [worker, stop_task], return_when=asyncio.FIRST_COMPLETED
        )
        if worker in done:
            worker.result()
            raise RuntimeError("dispatch consumer stopped unexpectedly")
    finally:
        worker.cancel()
        stop_task.cancel()
        await asyncio.gather(worker, stop_task, return_exceptions=True)
        await server.stop(grace=3)
        try:
            await asyncio.wait_for(nc.drain(), timeout=3)
        except Exception:
            log.warning("nats_drain_failed_during_shutdown", exc_info=True)
            await nc.close()


def main():
    asyncio.run(run())


if __name__ == "__main__":
    main()
