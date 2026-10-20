#!/bin/sh
set -eu
cd "$(dirname "$0")"
command -v docker >/dev/null
command -v curl >/dev/null
cleanup() {
  code=$?
  trap - EXIT
  if [ "$code" -ne 0 ]; then docker compose logs --tail=100 >&2 || true; fi
  if [ "${KEEP_LAB:-0}" != 1 ]; then docker compose down --remove-orphans; fi
  exit "$code"
}
trap cleanup EXIT
docker compose up --build -d
wait_for() {
  expected=$1
  url=$2
  attempt=0
  until [ "$(curl -fsS --max-time 2 "$url" 2>/dev/null || true)" = "$expected" ]; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then echo "timed out waiting for $expected at $url" >&2; exit 1; fi
    sleep 1
  done
}
wait_for ok http://127.0.0.1:19000/healthz
# Reset to blue so the test is repeatable even when containers already exist.
curl -fsS --max-time 3 -X POST 'http://127.0.0.1:19000/switch?backend=blue'
wait_for blue http://127.0.0.1:18080/
curl -fsS --max-time 3 -X POST 'http://127.0.0.1:19000/switch?backend=green'
wait_for green http://127.0.0.1:18080/
status=$(curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -X POST 'http://127.0.0.1:19000/switch?backend=purple')
[ "$status" = 400 ] || { echo "invalid backend returned $status" >&2; exit 1; }
wait_for green http://127.0.0.1:18080/
curl -fsS --max-time 3 http://127.0.0.1:19901/ready
echo "PASS: blue -> green, invalid backend rejected, Envoy ready"
