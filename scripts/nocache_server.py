#!/usr/bin/env python3
"""
nocache_server.py
-----------------------------------------------------------------------
Drop-in replacement for `python3 -m http.server <port>` that adds
Cache-Control/Pragma/Expires headers forcing the browser to never cache
any served file.

Why this exists: plain http.server sends Last-Modified but no
Cache-Control, so browsers heuristically cache JS modules (main.js,
state.js, etc.) and can keep serving a stale version across a normal
reload. That's indistinguishable from a real bug in the app code (the
easter egg appearing to "not reset" on reload when the actual, current
main.js would have reset it fine) and wastes time debugging the wrong
thing. This removes that entire class of false negative during dev.

Usage (same argv shape as `-m http.server`, so dev-up.sh only needs to
swap which command it runs):
    python3 nocache_server.py <port>

Process name still contains "http.server", matching dev-down.sh's
pgrep pattern (`http.server 5173` / `http.server 3000`), so shutdown
continues to work without changes there.
-----------------------------------------------------------------------
"""
import sys
from http.server import HTTPServer, SimpleHTTPRequestHandler


class NoCacheHandler(SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header('Cache-Control', 'no-store, no-cache, must-revalidate, max-age=0')
        self.send_header('Pragma', 'no-cache')
        self.send_header('Expires', '0')
        super().end_headers()


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8000
    server = HTTPServer(('', port), NoCacheHandler)
    print(f"nocache_server: serving on :{port} (http.server, caching disabled)")
    server.serve_forever()


if __name__ == '__main__':
    main()
