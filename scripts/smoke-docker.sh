#!/bin/sh
# Phase 8 acceptance on a clean machine (run by CI): build the image, start
# the demo, log in, and time how long until every simulated host has live
# data. Prints the seconds from `docker run` to live charts.
set -eu
IMAGE="${1:-sentrix:smoke}"
docker build -q -t "$IMAGE" "$(dirname "$0")/.." >/dev/null
START=$(date +%s.%N)
CID=$(docker run -d -p 127.0.0.1:18080:8080 "$IMAGE")
trap 'docker rm -f "$CID" >/dev/null' EXIT
API=http://127.0.0.1:18080/api/v1
PW=""
for _ in $(seq 1 120); do
    PW=$(docker logs "$CID" 2>&1 | sed -n 's/.*demo running.*password=\([^ ]*\).*/\1/p' | head -1)
    [ -n "$PW" ] && break
    sleep 0.5
done
[ -n "$PW" ] || { docker logs "$CID"; echo "FAIL: demo did not start"; exit 1; }
for _ in $(seq 1 120); do
    if curl -sf -c /tmp/sx-jar -H 'Origin: http://127.0.0.1:18080' -H 'Content-Type: application/json' \
        -d "{\"email\":\"demo@sentrix.local\",\"password\":\"$PW\"}" "$API/auth/login" >/dev/null; then
        LIVE=$(curl -sf -b /tmp/sx-jar "$API/hosts" | python3 -c 'import json,sys; print(sum(1 for h in json.load(sys.stdin)["hosts"] if h["latest"]))')
        if [ "$LIVE" -ge 4 ]; then
            python3 -c "print(f'PASS: docker run to login and live data on all 4 hosts: {$(date +%s.%N)-$START:.1f} s')"
            exit 0
        fi
    fi
    sleep 0.5
done
docker logs "$CID"; echo "FAIL: no live data within 60 s"; exit 1
