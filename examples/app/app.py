"""Demo app for the examples. Standard library only; runs as any UID with a read-only root filesystem.

  /            -> "hello from <hostname> build=<BUILD_ID> greeting=<GREETING>"
  /healthz     -> ok            (liveness)
  /ready       -> ok            (readiness)
  /slow?s=N    -> sleeps N seconds, then ok (Route timeout annotation)
  /env?k=NAME  -> value of env var NAME, or 404 (Secret injection)
  /burn?s=N    -> busy-loops N seconds (HPA)

Listens on $PORT (default 8080). The examples without a build mount this file into a stock UBI Python image with
Score `files:`; examples/quickstart has a copy that its BuildConfig bakes into an image.
"""
import os
import socket
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

BUILD_ID = open("/app/build-id").read().strip() if os.path.exists("/app/build-id") else "none"
PORT = int(os.environ.get("PORT", "8080"))


class Handler(BaseHTTPRequestHandler):
    def reply(self, code, body):
        data = (body + "\n").encode()
        self.send_response(code)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        u = urlparse(self.path)
        q = parse_qs(u.query)
        if u.path in ("/healthz", "/ready"):
            return self.reply(200, "ok")
        if u.path == "/slow":
            time.sleep(float(q.get("s", ["1"])[0]))
            return self.reply(200, "ok")
        if u.path == "/env":
            k = q.get("k", [""])[0]
            return self.reply(200, os.environ[k]) if k in os.environ else self.reply(404, "missing")
        if u.path == "/burn":
            end = time.time() + float(q.get("s", ["10"])[0])
            while time.time() < end:
                pass
            return self.reply(200, "ok")
        greeting = os.environ.get("GREETING", "-")
        return self.reply(200, f"hello from {socket.gethostname()} build={BUILD_ID} greeting={greeting}")

    def log_message(self, *args):
        pass


ThreadingHTTPServer(("", PORT), Handler).serve_forever()
