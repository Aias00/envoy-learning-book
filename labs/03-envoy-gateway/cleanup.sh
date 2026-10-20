#!/usr/bin/env bash
set -euo pipefail
# 只处理本书约定的独立 kind 集群；不删除当前上下文中的任何其他资源。
if ! kind get clusters | rg -x envoy-book >/dev/null; then
  echo '独立集群 envoy-book 不存在，无需清理。'
  exit 0
fi
kind delete cluster --name envoy-book
