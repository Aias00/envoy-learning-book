#!/usr/bin/env bash
set -euo pipefail
LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$LAB_DIR/common.sh"
LOCAL_PORT="${ENVOY_BOOK_TLS_PORT:-18443}"
TLS_SELECTOR='gateway.envoyproxy.io/owning-gateway-namespace=envoy-book,gateway.envoyproxy.io/owning-gateway-name=book-tls'
TLS_DIR="$(mktemp -d -t envoy-book-tls.XXXXXX)"
forward_pid=""
cleanup() {
  if [[ -n "$forward_pid" ]]; then
    kill "$forward_pid" 2>/dev/null || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  echo "实验自签证书和私钥保存在 ${TLS_DIR}；请勿提交私钥。"
}
trap cleanup EXIT
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 2 \
  -keyout "$TLS_DIR/tls.key" -out "$TLS_DIR/tls.crt" \
  -subj '/CN=secure.book.example' -addext 'subjectAltName=DNS:secure.book.example'
k -n envoy-book create secret tls book-tls --cert="$TLS_DIR/tls.crt" --key="$TLS_DIR/tls.key" \
  --dry-run=client -o yaml | k apply -f -
k apply -f "$LAB_DIR/tls.yaml"
wait_current_conditions httproute/secure envoy-book book-tls Accepted,ResolvedRefs
service_name=""
for attempt in $(seq 1 120); do
  service_name="$(k -n envoy-gateway-system get service -l "$TLS_SELECTOR" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -n "$service_name" ]]; then break; fi
  sleep 2
done
[[ -n "$service_name" ]] || { echo '未找到TLS数据面Service。' >&2; exit 1; }
k -n envoy-gateway-system rollout status deployment -l "$TLS_SELECTOR" --timeout=5m
k -n envoy-gateway-system port-forward --address 127.0.0.1 "service/$service_name" "$LOCAL_PORT:443" >"$TLS_DIR/port-forward.log" 2>&1 &
forward_pid=$!
wait_port_forward "$forward_pid" "$TLS_DIR/port-forward.log"
for attempt in $(seq 1 60); do
  if request --cacert "$TLS_DIR/tls.crt" \
    --resolve "secure.book.example:$LOCAL_PORT:127.0.0.1" \
    -fsS "https://secure.book.example:$LOCAL_PORT/"; then
    echo 'PASS：HTTPS入口完成证书校验并转发到HTTP后端。'
    exit 0
  fi
  kill -0 "$forward_pid" 2>/dev/null || { cat "$TLS_DIR/port-forward.log" >&2; exit 1; }
  sleep 2
done
echo 'TLS检查超时；检查Secret、listener conditions与port-forward日志。' >&2
exit 1
