from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from datetime import datetime, timedelta, timezone
import json
import os
import time

import boto3
from botocore.client import Config
from botocore.exceptions import ClientError

HOST = "0.0.0.0"
PORT = int(os.getenv("SIGNER_PORT", "19002"))
REGION = os.getenv("S3_REGION", "us-east-1")
ACCESS_KEY = os.environ["S3_ACCESS_KEY"]
SECRET_KEY = os.environ["S3_SECRET_KEY"]
INTERNAL_ENDPOINT = os.getenv("S3_ENDPOINT_INTERNAL", "http://minio:9000")
PUBLIC_ENDPOINT = os.getenv("S3_ENDPOINT_PUBLIC", "http://minio.localhost:29000")
BOOTSTRAP_BUCKETS = [b.strip() for b in os.getenv("S3_BOOTSTRAP_BUCKETS", "assets").split(",") if b.strip()]

cfg = Config(signature_version="s3v4", s3={"addressing_style": "path"})
internal = boto3.client(
    "s3", endpoint_url=INTERNAL_ENDPOINT, region_name=REGION,
    aws_access_key_id=ACCESS_KEY, aws_secret_access_key=SECRET_KEY, config=cfg,
)
public = boto3.client(
    "s3", endpoint_url=PUBLIC_ENDPOINT, region_name=REGION,
    aws_access_key_id=ACCESS_KEY, aws_secret_access_key=SECRET_KEY, config=cfg,
)

def iso(dt):
    if dt is None:
        return None
    return dt.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")

def expires_at(seconds):
    return iso(datetime.now(timezone.utc) + timedelta(seconds=seconds))

def wait_and_bootstrap():
    deadline = time.time() + 60
    while True:
        try:
            internal.list_buckets()
            for bucket in BOOTSTRAP_BUCKETS:
                try:
                    internal.head_bucket(Bucket=bucket)
                except ClientError:
                    internal.create_bucket(Bucket=bucket)
            print(f"component=minio-signer event=ready buckets={BOOTSTRAP_BUCKETS}", flush=True)
            return
        except Exception as exc:
            if time.time() >= deadline:
                raise
            print(f"component=minio-signer event=wait_minio error={exc}", flush=True)
            time.sleep(1)

class Handler(BaseHTTPRequestHandler):
    server_version = "gateway-runtime-minio-signer/0.1"

    def log_message(self, fmt, *args):
        print("component=minio-signer " + (fmt % args), flush=True)

    def send_json(self, status, payload=None):
        self.send_response(status)
        if payload is not None:
            data = json.dumps(payload, separators=(",", ":")).encode()
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        if payload is not None:
            self.wfile.write(data)

    def body(self):
        n = int(self.headers.get("Content-Length", "0"))
        return json.loads(self.rfile.read(n)) if n else {}

    def do_GET(self):
        if self.path == "/health":
            try:
                internal.list_buckets()
                self.send_json(200, {"status": "ok"})
            except Exception as exc:
                self.send_json(503, {"status": "not_ready", "error": str(exc)})
            return
        self.send_json(404, {"error": "not_found"})

    def do_POST(self):
        try:
            body = self.body()
            path = self.path
            bucket = body.get("bucket", "")
            key = body.get("object_key", "")
            ttl = int(body.get("expires_in_seconds", 900) or 900)

            if path == "/api/storage/presign/put":
                params = {"Bucket": bucket, "Key": key}
                content_type = body.get("content_type")
                headers = {}
                if content_type:
                    params["ContentType"] = content_type
                    headers["Content-Type"] = content_type
                url = public.generate_presigned_url("put_object", Params=params, ExpiresIn=ttl)
                self.send_json(200, {"url": url, "method": "PUT", "expires_at": expires_at(ttl), "headers": headers})
                return

            if path == "/api/storage/presign/get":
                url = public.generate_presigned_url("get_object", Params={"Bucket": bucket, "Key": key}, ExpiresIn=ttl)
                self.send_json(200, {"url": url, "method": "GET", "expires_at": expires_at(ttl), "headers": {}})
                return

            if path == "/api/storage/multipart/initiate":
                params = {"Bucket": bucket, "Key": key}
                if body.get("content_type"):
                    params["ContentType"] = body["content_type"]
                out = internal.create_multipart_upload(**params)
                self.send_json(200, {
                    "upload_id": out["UploadId"], "bucket": bucket, "object_key": key,
                    "expires_at": expires_at(ttl),
                })
                return

            if path == "/api/storage/multipart/part":
                params = {
                    "Bucket": bucket, "Key": key,
                    "UploadId": body["upload_id"], "PartNumber": int(body["part_number"]),
                }
                url = public.generate_presigned_url("upload_part", Params=params, ExpiresIn=ttl)
                self.send_json(200, {"url": url, "method": "PUT", "expires_at": expires_at(ttl), "headers": {}})
                return

            if path == "/api/storage/multipart/complete":
                parts = [{"PartNumber": int(p["part_number"]), "ETag": p["etag"]} for p in body["parts"]]
                out = internal.complete_multipart_upload(
                    Bucket=bucket, Key=key, UploadId=body["upload_id"],
                    MultipartUpload={"Parts": parts},
                )
                self.send_json(200, {
                    "etag": out.get("ETag", "").strip('"'),
                    "version_id": out.get("VersionId", ""),
                    "location": out.get("Location", f"s3://{bucket}/{key}"),
                })
                return

            if path == "/api/storage/multipart/abort":
                internal.abort_multipart_upload(Bucket=bucket, Key=key, UploadId=body["upload_id"])
                self.send_json(204)
                return

            if path == "/api/storage/metadata":
                try:
                    out = internal.head_object(Bucket=bucket, Key=key)
                except ClientError as exc:
                    code = str(exc.response.get("Error", {}).get("Code", ""))
                    status = exc.response.get("ResponseMetadata", {}).get("HTTPStatusCode")
                    if status == 404 or code in ("404", "NoSuchKey", "NotFound"):
                        self.send_json(200, {"exists": False, "bucket": bucket, "object_key": key})
                        return
                    raise
                checksums = {}
                for src, dst in (("ChecksumSHA256", "sha256"), ("ChecksumSHA1", "sha1"), ("ChecksumCRC32", "crc32"), ("ChecksumCRC32C", "crc32c")):
                    if out.get(src):
                        checksums[dst] = out[src]
                self.send_json(200, {
                    "exists": True,
                    "bucket": bucket,
                    "object_key": key,
                    "size": out.get("ContentLength", 0),
                    "content_type": out.get("ContentType", ""),
                    "etag": out.get("ETag", "").strip('"'),
                    "version_id": out.get("VersionId", ""),
                    "last_modified": iso(out.get("LastModified")),
                    "checksums": checksums,
                    "metadata": out.get("Metadata", {}),
                })
                return

            self.send_json(404, {"error": "not_found"})
        except Exception as exc:
            print(f"component=minio-signer event=error path={self.path} error={exc}", flush=True)
            self.send_json(502, {"error": "storage_provider_error", "detail": str(exc)})

if __name__ == "__main__":
    wait_and_bootstrap()
    print(f"component=minio-signer event=start addr=:{PORT} internal={INTERNAL_ENDPOINT} public={PUBLIC_ENDPOINT}", flush=True)
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()