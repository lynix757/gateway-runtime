from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from datetime import datetime, timedelta, timezone
from urllib.parse import urlparse

HOST = "0.0.0.0"
PORT = 19002

def now_plus(seconds=900):
    return (datetime.now(timezone.utc) + timedelta(seconds=seconds)).isoformat().replace("+00:00", "Z")

class Handler(BaseHTTPRequestHandler):
    server_version = "gateway-runtime-mock-storage/0.1"

    def log_message(self, fmt, *args):
        print("component=mock-storage " + (fmt % args), flush=True)

    def _json(self, status, payload=None):
        self.send_response(status)
        if payload is not None:
            data = json.dumps(payload).encode()
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        if payload is not None:
            self.wfile.write(data)

    def _read_json(self):
        length = int(self.headers.get("Content-Length", "0"))
        if length <= 0:
            return {}
        return json.loads(self.rfile.read(length))

    def do_GET(self):
        if urlparse(self.path).path == "/health":
            self._json(200, {"status": "ok"})
            return
        self._json(404, {"error": "not_found"})

    def do_POST(self):
        path = urlparse(self.path).path
        try:
            body = self._read_json()
        except Exception:
            self._json(400, {"error": "invalid_json"})
            return

        bucket = body.get("bucket", "assets")
        key = body.get("object_key", "demo.bin")
        ttl = int(body.get("expires_in_seconds", 900) or 900)

        if path == "/api/storage/presign/put":
            self._json(200, {
                "url": f"https://object.example.invalid/{bucket}/{key}?op=put",
                "method": "PUT",
                "expires_at": now_plus(ttl),
                "headers": {"Content-Type": body.get("content_type", "application/octet-stream")},
            })
            return

        if path == "/api/storage/presign/get":
            self._json(200, {
                "url": f"https://object.example.invalid/{bucket}/{key}?op=get",
                "method": "GET",
                "expires_at": now_plus(ttl),
                "headers": {},
            })
            return

        if path == "/api/storage/multipart/initiate":
            self._json(200, {
                "upload_id": "upload-demo-001",
                "bucket": bucket,
                "object_key": key,
                "expires_at": now_plus(ttl),
            })
            return

        if path == "/api/storage/multipart/part":
            part = int(body.get("part_number", 1))
            upload_id = body.get("upload_id", "upload-demo-001")
            self._json(200, {
                "url": f"https://object.example.invalid/{bucket}/{key}?uploadId={upload_id}&partNumber={part}",
                "method": "PUT",
                "expires_at": now_plus(ttl),
                "headers": {},
            })
            return

        if path == "/api/storage/multipart/complete":
            self._json(200, {
                "etag": "final-demo-etag",
                "version_id": "v-demo-001",
                "location": f"s3://{bucket}/{key}",
            })
            return

        if path == "/api/storage/multipart/abort":
            self._json(204)
            return

        if path == "/api/storage/metadata":
            exists = key != "missing.bin"
            payload = {
                "exists": exists,
                "bucket": bucket,
                "object_key": key,
            }
            if exists:
                payload.update({
                    "size": 1048576,
                    "content_type": "application/octet-stream",
                    "etag": "demo-etag",
                    "version_id": "v-demo-001",
                    "last_modified": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
                    "checksums": {"sha256": "demo-sha256"},
                    "metadata": {"environment": "compose-test"},
                })
            self._json(200, payload)
            return

        self._json(404, {"error": "not_found"})

if __name__ == "__main__":
    print(f"component=mock-storage event=start addr=:{PORT}", flush=True)
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
