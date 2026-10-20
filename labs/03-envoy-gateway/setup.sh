#!/usr/bin/env bash
set -euo pipefail
LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
for command_name in docker kind kubectl helm rg; do
  command -v "$command_name" >/dev/null || { echo "缺少命令: $command_name" >&2; exit 1; }
done
docker info >/dev/null
if ! kind get clusters | rg -x envoy-book >/dev/null; then
  kind create cluster --name envoy-book --config "$LAB_DIR/kind.yaml" --wait 180s
fi
kubectl --context kind-envoy-book cluster-info
helm upgrade --install eg oci://docker.io/envoyproxy/gateway-helm \
  --version v1.9.2 --kube-context kind-envoy-book \
  -n envoy-gateway-system --create-namespace --wait --timeout 10m
kubectl --context kind-envoy-book apply -f "$LAB_DIR/base.yaml"
kubectl --context kind-envoy-book -n envoy-book rollout status deployment/backend --timeout=5m
kubectl --context kind-envoy-book wait gatewayclass/envoy-book --for=condition=Accepted --timeout=5m
echo "基础资源已提交；继续执行 bash labs/03-envoy-gateway/smoke.sh。"
