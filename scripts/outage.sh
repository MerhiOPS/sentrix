#!/bin/sh
# Phase 6 acceptance with the real binaries: kill -9 the server for two
# minutes while an agent keeps sampling, restart it, and check that every
# 5 s slot from the first sample to the last was stored exactly once.
#
#   scripts/outage.sh [down-seconds]      (default 120)
#
# Builds both binaries, uses a throwaway directory and ports 28443/28080.
set -eu
DOWN="${1:-120}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
W="$(mktemp -d)"
API=http://127.0.0.1:28080/api/v1
PW=outage-test-password-123
cleanup() { kill "$AGENT" "$SERVER" 2>/dev/null || true; }
trap cleanup EXIT

(cd "$ROOT" && go build -o "$W/sentrix-server" ./cmd/sentrix-server && go build -o "$W/sentrix-agent" ./cmd/sentrix-agent)
cd "$W"
TID=$(./sentrix-server tenant create --data data --name outage | grep -oE '[0-9a-f-]{36}' | head -1)
echo "$PW" | ./sentrix-server user create --data data --tenant "$TID" --email ops@outage.test >/dev/null
TOKEN=$(./sentrix-server token create --data data --tenant "$TID" | grep -oE 'stx1\.[^ ]+' | head -1)

serve() { ./sentrix-server serve --data data --listen 127.0.0.1:28443 --api-listen 127.0.0.1:28080 >>server.log 2>&1 & SERVER=$!; sleep 2; }
serve
./sentrix-agent enroll --server https://127.0.0.1:28443 --token "$TOKEN" --state-dir agent --interval 5s >/dev/null
./sentrix-agent run --state-dir agent --interval 5s >>agent.log 2>&1 &
AGENT=$!

echo "up 30 s, then kill -9 the server for ${DOWN} s"
sleep 30
kill -9 "$SERVER"
sleep "$DOWN"
echo "server back; letting the agent replay for 90 s"
serve
sleep 90

curl -s -c jar -H 'Origin: http://127.0.0.1:28080' -H 'Content-Type: application/json' \
    -d "{\"email\":\"ops@outage.test\",\"password\":\"$PW\"}" "$API/auth/login" >/dev/null
HOST=$(curl -s -b jar "$API/hosts" | python3 -c 'import json,sys; print(json.load(sys.stdin)["hosts"][0]["id"])')
FROM=$(date -u -d '-30 min' +%Y-%m-%dT%H:%M:%SZ)
TO=$(date -u -d '+1 min' +%Y-%m-%dT%H:%M:%SZ)
curl -s -b jar "$API/hosts/$HOST/metrics?gauges=cpu&from=$FROM&to=$TO&step=5" > frame.json
curl -s -b jar "$API/hosts/$HOST" > host.json
python3 - "$DOWN" <<'PY'
import json, sys
down = int(sys.argv[1])
f = json.load(open("frame.json"))
cpu = f["series"]["cpu"]
filled = [i for i, v in enumerate(cpu) if v is not None]
first, last = filled[0], filled[-1]
gaps = [i for i in range(first, last + 1) if cpu[i] is None]
h = json.load(open("host.json"))["health"]
span = (last - first + 1) * 5
print(f"slots from first to last sample: {last - first + 1} ({span} s, outage {down} s)")
print(f"missing slots: {len(gaps)}   conflicts (re-applied samples): {h['conflict_total']}   agent drops: {h['dropped_total']}")
ok = not gaps and h["conflict_total"] == 0 and h["dropped_total"] == 0 and span > down
print("PASS" if ok else "FAIL")
sys.exit(0 if ok else 1)
PY
