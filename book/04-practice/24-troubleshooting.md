# 24　用证据分层排障

本章目标：面对“网关不通”时，先定位失败阶段，再选择工具；把 Kubernetes 意图、控制平面配置和真实请求连接起来。先修：完成至少一个实验，理解资源图、ACK 与 Gateway API 状态。

## 先记录问题本身

“访问失败”信息太少。至少记录时间、请求方法、完整 URL（移除令牌等敏感信息）、Host/SNI、客户端所处网络、HTTP 状态或连接错误、响应头、响应体摘要。区分从未成功、部署后失败、偶发失败和只有某类请求失败。一次变更的时间点也有价值，但相关不等于因果，要通过实际配置和请求验证。

下面是一条常用观察命令，端口应替换为当前实验入口：

```sh
curl -v --connect-timeout 3 --max-time 10 \
  -H 'Host: book.example' http://127.0.0.1:18888/
```

它能显示连接是否建立、发出的 Host 和收到的 HTTP。不要把带凭证的 verbose 输出直接贴到公共 issue。对 HTTPS 路由，使用 `--resolve` 同时控制连接地址、URL 域名与 SNI，见第 20 章；`-k` 会跳过证书校验，只适合明确的临时定位，不能用来证明 TLS 配置正确。

## 沿链路逐层取证

| 观察到的现象 | 首先检查 | 下一步证据 |
| --- | --- | --- |
| 域名无法解析 | DNS 名字、搜索域、运行环境 | 解析结果与实际目标 |
| Connection refused | 端口监听、映射、进程 | Compose ps、port-forward 日志 |
| TLS 握手/证书错误 | SNI、证书链、有效期、信任根 | 证书内容、curl TLS 输出 |
| HTTP 404 | Host、路径、路由接入、响应来源 | HTTPRoute status、Envoy路由 |
| HTTP 503 | Cluster、端点、健康与连接 | EndpointSlice、clusters、响应标记 |
| HTTP 504/超时 | 各段超时、后端耗时、重试 | 访问日志、时延与上游指标 |
| 配置一直旧版本 | 生成、分组、订阅、NACK | 控制平面日志、config_dump |

表格给出调查方向，而不是状态码的唯一解释。例如 404 也可能来自业务应用，503 也可能来自应用维护逻辑。优先通过代理访问日志判断谁产生响应，再解释原因。Envoy 响应标记如 NR、UH、UF 提供线索，但要按对应版本文档解释；不能仅凭一个标记推断所有根因。

## Kubernetes 的五站证据

第一站是 API 对象是否存在，namespace 和 context 是否正确。先运行 `kubectl --context kind-envoy-book get ...`，避免在另一集群里寻找实验资源。第二站是配置是否被控制器接受：GatewayClass、Gateway、HTTPRoute 的 status 条件分别代表不同事实。HTTPRoute 应检查正确 parent 对应的 Accepted 和 ResolvedRefs，以及 observedGeneration 是否跟上对象 generation。`Accepted=True` 不等于每一个引用都已正确解析。

第三站是基础设施。EG 控制器 Deployment 健康，不等于它管理的 Envoy Deployment 就绪。查看两者 Pod、事件、镜像拉取和 Service。kind 中 LoadBalancer 的外部 IP 可能一直 pending，实验通过 port-forward 避开该依赖，不应据此宣称所有云上流量入口都会工作。

第四站是后端端点。Service 存在并不等于有 Ready Pod 被 selector 选中；查看 EndpointSlice，检查端口、地址、Ready 和 Pod 的实际监听。第五站才是 Envoy 生效配置与请求。在这一站确认 Host/path 是否命中、Cluster 与端点是否出现，并发真实请求。少任何一站，都可能把“配置对象提交成功”误认为“网关已工作”。

```sh
kubectl --context kind-envoy-book -n envoy-book get gateway,httproute -o yaml
kubectl --context kind-envoy-book -n envoy-book get pod,service,endpointslice
kubectl --context kind-envoy-book -n envoy-gateway-system get deploy,pod,service
kubectl --context kind-envoy-book -n envoy-gateway-system logs deployment/envoy-gateway --tail=100
```

最后一个 Deployment 名称以实际 Helm 安装结果为准；如果名字不同，先 `get deploy` 再选择，不要因日志命令找不到对象就判断控制器崩溃。

## xDS 的五站证据

自己编写控制平面时，检查顺序是：意图是否正确 → 资源图是否正确 → snapshot 是否发布到正确 key → Envoy 是否订阅并接受 → 业务请求是否命中。`SetSnapshot` 成功仍不足以证明 Envoy 收到，Node.ID 不匹配时甚至可能只是缓存里多了一份无人订阅的配置。

如果出现 NACK，收集资源 type URL、候选版本、nonce、节点和错误详情，再定位资源构造，不要靠增加版本号掩盖无效字段。若没有 NACK 但配置仍旧，应核对是否发送了新内容、版本是否变化、订阅名字是否匹配、连接是否已断开。若配置已切到 green 但响应仍异常，继续查 green Cluster 和后端，问题可能已离开配置分发层。

在实验二里，可以用 Admin 的 config_dump 搜索 `book_routes`、`blue_backend` 和 `green_backend`。注意 dump 可能同时含 active/warming 等不同状态，不能只因为搜到一个字符串就证明正在使用它。看对象所处的上下文、版本和激活状态，再与请求结果交叉确认。

## 故障复盘要保留最小因果链

一份有用的记录包括：触发条件、第一条异常证据、错误实际所在层、修复改动、恢复验证和防止再次发生的检查。例如“HTTPRoute 引用 backend 的 8080，但 Service 暴露 3000；ResolvedRefs=False；改为 3000 后引用解析成功且请求返回 backend 标识”。这比“重启了一下就好了”更有学习价值。

重启可能暂时改变时序、DNS 或端点状态，但也可能清除最有价值的日志。先保存只读证据，再对独立实验做修改；生产环境则遵循所在团队的变更和应急流程。

## 练习与参考答案

1. EG Deployment Ready，HTTPRoute Accepted=True，是否可宣布请求可用？
2. 业务请求仍是 blue，管理接口已返回成功，为什么应该先看 config_dump？
3. 多次重试能否修复错误的 Service 端口引用？

<details><summary>参考答案</summary>

1. 还要检查正确 parent 的 ResolvedRefs、数据平面基础设施、后端端点和真实请求。
2. 它帮助区分配置未生效与生效后请求未命中。随后仍要核对 active 状态、Host/path 和上游结果。
3. 不能。重试只会重复错误访问，应修复声明与实际 Service 的对应关系。

</details>

来源：[Envoy 响应标记](https://www.envoyproxy.io/docs/envoy/v1.36.0/configuration/observability/access_log/usage)、[EG v1.9 排障](https://github.com/envoyproxy/gateway/tree/v1.9.2/site/content/en/v1.9/troubleshooting)、[实验与验证记录](../../VALIDATION.md)。
