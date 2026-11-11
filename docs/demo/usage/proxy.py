"""A proxy that plays api.anthropic.com's usage endpoint for usage.tape.

Every CONNECT is answered by this process itself, under the demo CA's
certificate: the token a sign-in sends picks which made-up limits come back.
"""

import json
import socketserver
import ssl
import sys
from datetime import datetime, timedelta, timezone


def at(**delta):
    return (datetime.now(timezone.utc) + timedelta(**delta)).isoformat()


def limits(token):
    if token == "demo-work":
        return {
            "five_hour": {"utilization": 12, "resets_at": at(hours=4, minutes=12)},
            "seven_day": {"utilization": 31, "resets_at": at(days=4, hours=6)},
        }

    return {
        "five_hour": {"utilization": 38, "resets_at": at(hours=3, minutes=4)},
        "seven_day": {"utilization": 96, "resets_at": at(hours=9, minutes=44)},
        "limits": [
            {"kind": "weekly_scoped", "percent": 64, "resets_at": at(hours=9, minutes=44),
             "scope": {"model": {"display_name": "Opus"}}},
        ],
    }


def head(f):
    lines = []
    while (line := f.readline()) not in (b"\r\n", b"\n", b""):
        lines.append(line.decode("latin-1").strip())
    return lines


class Handler(socketserver.StreamRequestHandler):
    def handle(self):
        if not head(self.rfile):
            return
        self.wfile.write(b"HTTP/1.1 200 Connection established\r\n\r\n")
        self.wfile.flush()
        tls = ctx.wrap_socket(self.connection, server_side=True)
        f = tls.makefile("rwb")
        token = ""
        for line in head(f):
            name, _, value = line.partition(":")
            if name.lower() == "authorization":
                token = value.strip().removeprefix("Bearer ")
        body = json.dumps(limits(token)).encode()
        f.write(b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nConnection: close\r\n")
        f.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
        f.flush()
        tls.close()


ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(sys.argv[2], sys.argv[3])
socketserver.ThreadingTCPServer.allow_reuse_address = True
socketserver.ThreadingTCPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
