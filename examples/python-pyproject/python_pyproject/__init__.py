from http.server import BaseHTTPRequestHandler, HTTPServer
import os


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.end_headers()
        self.wfile.write(
            b"<!doctype html><title>skiff</title><h1>hello from skiff (python, pyproject.toml)</h1>"
        )


def main():
    port = int(os.environ.get("PORT", "8000"))
    print(f"python-pyproject listening on {port}", flush=True)
    HTTPServer(("0.0.0.0", port), Handler).serve_forever()
