#!/usr/bin/env bash
# Runs smoke.sh against a fake api; FAIL=<mode> breaks one behavior.
set -euo pipefail
dir=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'kill "${pid:-}" 2>/dev/null; rm -rf "$tmp"' EXIT

cat >"$tmp/fake.py" <<'PY'
import json, os
from http.server import BaseHTTPRequestHandler, HTTPServer

fail = os.environ.get("FAIL", "")

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
    def do_GET(self):
        authed = self.headers.get("Authorization") == "Bearer k"
        if self.path == "/healthz":
            self.send(503 if fail == "health" else 200, {"ok": True})
        elif self.path == "/config.json":
            self.send(200, {"workosClientId": "c"})
        elif self.path == "/v1/models":
            if not authed:
                self.send(200 if fail == "open" else 401, {})
            else:
                self.send(200, {"data": [{"id": "other" if fail == "model" else "m1"}]})
        else:
            self.send(404, {})
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if fail == "chat":
            self.send(502, {"error": "x"})
        else:
            self.send(200, {"choices": [{"message": {"content": "hi"}}]})

s = HTTPServer(("127.0.0.1", 0), H)
print(s.server_port, flush=True)
s.serve_forever()
PY

run() {
  FAIL=$1 python3 -I "$tmp/fake.py" >"$tmp/port" &
  pid=$!
  until [ -s "$tmp/port" ]; do sleep 0.1; done
  local rc=0
  BASE_URL="http://127.0.0.1:$(cat "$tmp/port")" PROXY_KEY=k MODEL=m1 "$dir/smoke.sh" >"$tmp/out" 2>&1 || rc=$?
  kill "$pid"; wait "$pid" 2>/dev/null || true; : >"$tmp/port"
  return $rc
}

run "" || { cat "$tmp/out"; echo "pass path failed"; exit 1; }
for mode in health open model chat; do
  if run "$mode"; then echo "mode $mode: want failure"; exit 1; fi
  grep -q FAIL "$tmp/out" || { echo "mode $mode: no FAIL printed"; exit 1; }
done
echo "smoke_test ok"
