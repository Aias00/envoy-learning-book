#!/usr/bin/env bash
set -euo pipefail
LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$LAB_DIR/common.sh"
for command_name in docker kind kubectl helm rg jq; do
  command -v "$command_name" >/dev/null || { echo "缺少命令: $command_name" >&2; exit 1; }
done
docker info >/dev/null
if ! kind get clusters | rg -x envoy-book >/dev/null; then
  kind create cluster --name envoy-book --config "$LAB_DIR/kind.yaml" --wait 180s
fi
kubectl --context kind-envoy-book cluster-info
chart_source="${ENVOY_BOOK_CHART:-oci://docker.io/envoyproxy/gateway-helm}"
if [[ -n "${ENVOY_BOOK_CHART:-}" ]]; then
  [[ -f "$chart_source" ]] || { echo "本地chart不存在：${chart_source}" >&2; exit 1; }
  helm show chart "$chart_source" | rg '^version: "?v?1\.9\.2"?$' >/dev/null || {
    echo '本地chart必须是官方v1.9.2版本。' >&2; exit 1;
  }
fi
helm upgrade --install eg "$chart_source" \
  --version v1.9.2 --kube-context kind-envoy-book \
  -n envoy-gateway-system --create-namespace --wait --timeout 10m
k apply -f "$LAB_DIR/base.yaml"
k -n envoy-book rollout status deployment/backend --timeout=5m
wait_current_conditions gatewayclass/envoy-book envoy-book '' Accepted
echo "基础资源已提交；继续执行 bash labs/03-envoy-gateway/smoke.sh。"
