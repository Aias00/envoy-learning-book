#!/usr/bin/env bash
# 由实验脚本 source；所有Kubernetes访问固定到本书独立集群。
k() { kubectl --context kind-envoy-book --request-timeout=15s "$@"; }

wait_current_conditions() {
  local resource_name="$1" namespace_name="$2" parent_name="$3" condition_names="$4"
  local attempt
  for attempt in $(seq 1 150); do
    if k -n "$namespace_name" get "$resource_name" -o json | jq -e \
      --arg parent "$parent_name" --arg conditions "$condition_names" '
        . as $object |
        (if $parent == "" then [.status]
         elif .status.parents then
           [.status.parents[] | select(.parentRef.name == $parent and
             (.parentRef.namespace // $object.metadata.namespace) == "envoy-book" and
             .controllerName == "gateway.envoyproxy.io/gatewayclass-controller")]
         else
           [.status.ancestors[]? | select(.ancestorRef.name == $parent and
             (.ancestorRef.namespace // $object.metadata.namespace) == "envoy-book" and
             .controllerName == "gateway.envoyproxy.io/gatewayclass-controller")]
         end) as $entries |
        ($entries | length) > 0 and
        all($conditions | split(",")[]; . as $condition |
          any($entries[].conditions[]?;
            .type == $condition and .status == "True" and
            .observedGeneration == $object.metadata.generation))
      ' >/dev/null; then
      echo "${resource_name}：当前generation的${condition_names}条件已满足。"
      return 0
    fi
    sleep 2
  done
  k -n "$namespace_name" get "$resource_name" -o yaml >&2 || true
  echo "等待${resource_name}条件超时。" >&2
  return 1
}

wait_port_forward() {
  local process_id="$1" log_file="$2" attempt
  for attempt in $(seq 1 60); do
    if ! kill -0 "$process_id" 2>/dev/null; then
      cat "$log_file" >&2
      echo '本次port-forward已退出，可能是端口被占用。' >&2
      return 1
    fi
    if rg -q '^Forwarding from ' "$log_file"; then
      return 0
    fi
    sleep 1
  done
  cat "$log_file" >&2
  echo '等待本次port-forward监听超时。' >&2
  return 1
}

request() { curl --noproxy '*' --connect-timeout 2 --max-time 5 "$@"; }
