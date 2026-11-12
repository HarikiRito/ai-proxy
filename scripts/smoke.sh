#!/usr/bin/env bash
# Smoke test a deployed api. Env: BASE_URL, PROXY_KEY, optional MODEL (default: first listed), STREAM=1.
set -euo pipefail
base=${BASE_URL:-${1:-}}
key=${PROXY_KEY:-${2:-}}
[ -n "$base" ] && [ -n "$key" ] || { echo "usage: BASE_URL=... PROXY_KEY=... $0" >&2; exit 2; }
base=${base%/}
failed=0
body=$(mktemp)
trap 'rm -f "$body"' EXIT

# status <want> <method> <path> [curl args...]: response body lands in $body
status() {
  local want=$1 method=$2 path=$3 got
  shift 3
  got=$(curl -sS --max-time 60 -o "$body" -w '%{http_code}' -X "$method" "$base$path" "$@") || return 1
  [ "$got" = "$want" ]
}

# step <name> <cmd...>
step() {
  local name=$1
  shift
  if "$@"; then echo "PASS $name"; else echo "FAIL $name"; failed=1; fi
}

body_has() { grep -q "$1" "$body"; }

auth=(-H "Authorization: Bearer $key")
chat=(-H "Content-Type: application/json" "${auth[@]}")

step "GET /healthz 200" status 200 GET /healthz
step "GET /config.json 200" status 200 GET /config.json
step "GET /config.json is json" body_has '^[[:space:]]*{'
step "GET /v1/models without key 401" status 401 GET /v1/models
step "GET /v1/models 200" status 200 GET /v1/models "${auth[@]}"

model=${MODEL:-$(grep -o '"id" *: *"[^"]*"' "$body" | head -1 | sed 's/.*: *"\(.*\)"/\1/' || true)}
step "model ${model:-?} listed" body_has "\"${model:-?}\""

payload="{\"model\":\"$model\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"Say hi\"}]"
step "POST /v1/chat/completions 200" status 200 POST /v1/chat/completions "${chat[@]}" -d "$payload}"
step "chat completion has choices" body_has '"choices"'

if [ "${STREAM:-}" = 1 ]; then
  step "POST /v1/chat/completions stream 200" status 200 POST /v1/chat/completions "${chat[@]}" -d "$payload,\"stream\":true}"
  step "stream has data events" body_has '^data:'
fi

exit "$failed"
