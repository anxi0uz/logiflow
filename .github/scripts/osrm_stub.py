"""Deterministic OSRM route response for the CI HTTP end-to-end test."""

import json
import signal
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlsplit


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        path = urlsplit(self.path).path
        if not path.startswith("/route/v1/driving/"):
            self.send_error(404)
            return
        try:
            coordinates = [
                [float(value) for value in point.split(",")]
                for point in path.removeprefix("/route/v1/driving/").split(";")
            ]
            if len(coordinates) != 2 or any(len(point) != 2 for point in coordinates):
                raise ValueError("expected two longitude,latitude pairs")
        except ValueError:
            self.send_error(400)
            return
        payload = json.dumps(
            {
                "routes": [
                    {
                        "geometry": {"coordinates": coordinates},
                        "distance": 25000,
                        "duration": 900,
                    }
                ]
            }
        ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, _format, *_args):
        pass


def stop(_signal, _frame):
    raise SystemExit(0)


signal.signal(signal.SIGTERM, stop)
HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
