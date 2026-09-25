import asyncio
import signal
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock

import pytest
from nats.errors import ConnectionReconnectingError


@pytest.mark.parametrize("stop_kind", ["signal", "worker_failure", "nats_unavailable"])
def test_service_stops_workers_and_grpc(monkeypatch, stop_kind):
    monkeypatch.setenv("DATABASE_URL", "postgresql+psycopg://test:test@localhost/test")
    from document_service import main as app

    async def scenario():
        loop = asyncio.get_running_loop()
        handlers = {}
        monkeypatch.setattr(
            loop,
            "add_signal_handler",
            lambda sig, callback: handlers.setdefault(sig, callback),
        )

        class Server:
            def __init__(self):
                self.started = asyncio.Event()
                self.stopped = asyncio.Event()

            def add_insecure_port(self, _address):
                return 50051

            async def start(self):
                self.started.set()

            async def wait_for_termination(self):
                await self.stopped.wait()

            async def stop(self, grace):
                assert grace == 3
                self.stopped.set()

        server = Server()
        nc = MagicMock()
        js = MagicMock()
        js.stream_info = AsyncMock()
        nc.jetstream.return_value = js
        nc.drain = AsyncMock()
        nc.close = AsyncMock()
        if stop_kind == "nats_unavailable":
            nc.drain.side_effect = ConnectionReconnectingError()
            nc.close.side_effect = ConnectionResetError("connection lost")
        monkeypatch.setattr(app.nats, "connect", AsyncMock(return_value=nc))
        monkeypatch.setattr(app.grpc.aio, "server", lambda: server)
        monkeypatch.setattr(
            app.documents_pb2_grpc,
            "add_DocumentInboxServicer_to_server",
            lambda *_: None,
        )
        monkeypatch.setattr(
            app,
            "get_settings",
            lambda: SimpleNamespace(
                nats_url="nats://test:4222",
                grpc_host="127.0.0.1",
                grpc_port=50051,
                font_path="/unused.ttf",
            ),
        )
        engine = SimpleNamespace(dispose=AsyncMock())
        monkeypatch.setattr(app, "engine", engine)

        async def wait_forever(*_args):
            await asyncio.Event().wait()

        async def fail_worker(*_args):
            raise RuntimeError("consumer failed")

        monkeypatch.setattr(
            app,
            "consume_deliveries",
            fail_worker if stop_kind == "worker_failure" else wait_forever,
        )
        monkeypatch.setattr(app, "publish_ready_events", wait_forever)

        async def signal_after_start():
            await server.started.wait()
            handlers[signal.SIGTERM]()

        if stop_kind != "worker_failure":
            trigger = asyncio.create_task(signal_after_start())
            await asyncio.wait_for(app.run(), timeout=2)
            await trigger
        else:
            with pytest.raises(RuntimeError, match="consumer failed"):
                await asyncio.wait_for(app.run(), timeout=2)
        assert server.stopped.is_set()
        nc.drain.assert_awaited_once()
        if stop_kind == "nats_unavailable":
            nc.close.assert_awaited_once()
        engine.dispose.assert_awaited_once()

    asyncio.run(scenario())
