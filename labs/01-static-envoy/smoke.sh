#!/bin/sh
set -eu
cd "$(dirname "$0")"
command -v docker >/dev/null
command -v curl >/dev/null
cleanup() {
  code=$?
  trap - EXIT
  if [ "$code" -ne 0 ]; then docker compose logs --tail=80 >&2 || true; fi
  if [ "${KEEP_LAB:-0}" != 1 ]; then docker compose down --remove-orphans; fi
  exit "$code"
}
trap cleanup EXIT
docker compose up --build -d
for path in / /green; do
  expected=blue
  [ "$path" != /green ] || expected=green
  attempt=0
  until [ "$(curl -fsS --max-time 2 "http://127.0.0.1:18000$path" 2>/dev/null || true)" = "$expected" ]; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then echo "timed out waiting for $expected" >&2; exit 1; fi
    sleep 1
  done
done
curl -fsS --max-time 3 http://127.0.0.1:19900/ready
echo "PASS: / -> blue, /green -> green, Envoy ready"
