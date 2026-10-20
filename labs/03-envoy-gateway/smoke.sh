#!/usr/bin/env bash
set -euo pipefail
LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KUBE_CONTEXT=kind-envoy-book
LOCAL_PORT="${ENVOY_BOOK_PORT:-18888}"
PROXY_SELECTOR='gateway.envoyproxy.io/owning-gateway-namespace=envoy-book,gateway.envoyproxy.io/owning-gateway-name=book'
forward_pid=""
temporary_dir="$(mktemp -d -t envoy-book-smoke.XXXXXX)"
cleanup() {
  if [[ -n "$forward_pid" ]]; then kill "$forward_pid" 2>/dev/null || true; fi
  echo "port-forward 日志保留在 ${temporary_dir}；集群保留以便观察。"
}
trap cleanup EXIT
k() { kubectl --context "$KUBE_CONTEXT" "$@"; }
command -v rg >/dev/null || { echo '请先安装 ripgrep（rg）。' >&2; exit 1; }
bash "$LAB_DIR/setup.sh"
k apply -f "$LAB_DIR/routing.yaml" -f "$LAB_DIR/ratelimit.yaml"
k -n envoy-book wait httproute/backend httproute/routing httproute/limited \
  --for=jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}'=True --timeout=5m
service_name=""
for attempt in $(seq 1 120); do
  service_name="$(k -n envoy-gateway-system get service -l "$PROXY_SELECTOR" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -n "$service_name" ]]; then break; fi
  sleep 2
done
[[ -n "$service_name" ]] || { echo '未找到数据面 Service。' >&2; exit 1; }
k -n envoy-gateway-system rollout status deployment -l "$PROXY_SELECTOR" --timeout=5m
k -n envoy-gateway-system port-forward "service/$service_name" "$LOCAL_PORT:80" >"$temporary_dir/port-forward.log" 2>&1 &
forward_pid=$!
for attempt in $(seq 1 60); do
  if curl --noproxy '*' -fsS -H 'Host: book.example' "http://127.0.0.1:$LOCAL_PORT/" >"$temporary_dir/body.json"; then break; fi
  kill -0 "$forward_pid" 2>/dev/null || { cat "$temporary_dir/port-forward.log" >&2; exit 1; }
  sleep 2
done
curl --noproxy '*' -fsS -H 'Host: book.example' "http://127.0.0.1:$LOCAL_PORT/" | rg 'backend-'
unknown_status="$(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -H 'Host: unknown.example' "http://127.0.0.1:$LOCAL_PORT/")"
[[ "$unknown_status" == 404 ]] || { echo "未知主机预期404，实际$unknown_status" >&2; exit 1; }
header_seen=false
for attempt in $(seq 1 60); do
  if curl --noproxy '*' -fsS -D "$temporary_dir/headers.txt" -o /dev/null \
    -H 'Host: routing.example' -H 'x-book-mode: debug' \
    "http://127.0.0.1:$LOCAL_PORT/api" && rg -i '^x-book-route: debug-api' "$temporary_dir/headers.txt"; then
    header_seen=true
    break
  fi
  sleep 1
done
[[ "$header_seen" == true ]] || { echo '未观察到请求头路由，检查发布状态。' >&2; exit 1; }
limited_seen=false
for attempt in $(seq 1 15); do
  result="$(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -H 'Host: limited.example' "http://127.0.0.1:$LOCAL_PORT/")"
  echo "限流探测 $attempt: HTTP $result"
  if [[ "$result" == 429 ]]; then limited_seen=true; break; fi
  sleep 1
done
[[ "$limited_seen" == true ]] || { echo '未观察到429，请查看策略status与Envoy配置。' >&2; exit 1; }
k -n envoy-book get gateway,httproute,backendtrafficpolicy
k -n envoy-book get endpointslice -l kubernetes.io/service-name=backend
echo 'PASS：基础转发、未知Host 404、精确请求头路由、本地限流429。TLS为独立可选实验。'
