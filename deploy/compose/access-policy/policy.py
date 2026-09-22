from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os

HOST = "0.0.0.0"
PORT = int(os.getenv("ACCESS_POLICY_PORT", "19100"))

SUBJECT_ROLES = json.loads(os.getenv(
    "ACCESS_POLICY_SUBJECT_ROLES",
    '{"alice":["asset-user"],"auditor":["auditor"],"admin":["archive-admin"]}'
))
ROLE_RULES = json.loads(os.getenv(
    "ACCESS_POLICY_ROLE_RULES",
    '{"asset-user":["storage.upload:asset","storage.download:asset","storage.metadata:asset","storage.multipart:asset"],'
    '"auditor":["storage.download:audit-evidence","storage.metadata:audit-evidence"],'
    '"archive-admin":["storage.download:archive","storage.metadata:archive"]}'
))

def is_allowed(subject, action, target):
    roles = SUBJECT_ROLES.get(subject, [])
    wanted = f"{action}:{target}"
    for role in roles:
        rules = ROLE_RULES.get(role, [])
        if wanted in rules or f"{action}:*" in rules or f"*:{target}" in rules or "*:*" in rules:
            return True, role
    return False, None

class Handler(BaseHTTPRequestHandler):
    server_version = "gateway-runtime-access-policy/0.1"

    def log_message(self, fmt, *args):
        print("component=access-policy " + (fmt % args), flush=True)

    def send_json(self, status, payload):
        data = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def body(self):
        n = int(self.headers.get("Content-Length", "0"))
        return json.loads(self.rfile.read(n)) if n else {}

    def do_GET(self):
        if self.path == "/health":
            self.send_json(200, {"status": "ok"})
            return
        self.send_json(404, {"error": "not_found"})

    def do_POST(self):
        if self.path != "/v1/authorize":
            self.send_json(404, {"error": "not_found"})
            return
        try:
            body = self.body()
            subject = str(body.get("subject", "")).strip()
            action = str(body.get("action", "")).strip()
            target = str(body.get("target", "")).strip()
            if not subject or not action or not target:
                self.send_json(400, {"error": "invalid_request"})
                return
            allowed, role = is_allowed(subject, action, target)
            if allowed:
                self.send_json(200, {"allow": True, "reason": f"allowed_by_role:{role}"})
            else:
                self.send_json(200, {"allow": False, "reason": "target_not_allowed"})
        except Exception as exc:
            print(f"component=access-policy event=error error={exc}", flush=True)
            self.send_json(500, {"error": "policy_error"})

if __name__ == "__main__":
    print(f"component=access-policy event=start addr=:{PORT}", flush=True)
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
