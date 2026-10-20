# 25　综合项目：一套可解释的蓝绿网关

本章目标：把三个实验串成一个项目，独立解释“谁决定流量、谁执行转发、如何证明变化”。本章不是再复制一个大型工程；交付的是一份可复现的学习项目与证据笔记。先修：三篇核心章节与第 24 章。

## 项目需求

你负责一个图书服务。用户通过稳定入口访问 `/`，后端有 blue 与 green 两个版本。初始流量去 blue，你需要切到 green，再切回 blue。要求路由意图、配置变更、实际响应三方面都能解释，还要主动制造一次错误并说明为什么失败。三个实现阶段分别使用静态文件、自己的 Go 控制平面和 EG 管理的 Gateway API。

最终笔记至少包括一张组件图、一张资源引用表、三次切换的步骤和证据、一个失败案例及恢复过程。请保留工具与镜像版本。不能只交“运行脚本显示 PASS”，因为这无法说明你是否理解脚本验证的范围。

## 阶段一：静态配置

运行[实验一](../../labs/01-static-envoy/README.md)，先确认 blue。把兜底 route 的 Cluster 改为 green，用 `docker compose up -d --force-recreate envoy` 重建 Envoy 容器，等待就绪，再核对 config_dump 与响应。然后恢复默认文件与 blue 状态。

你的解释应包含：Listener 内部端口与宿主机映射、HCM 如何找到路由、route.cluster 与 Cluster.name、STRICT_DNS 如何解析 Compose 服务名。切换时后端都存在，改动的是请求选择，而不是复制整个业务进程。重启是这个静态配置加载方式需要的步骤，不是“所有 Envoy 配置改变都必须重启”。

## 阶段二：Go xDS

运行[实验二](../../labs/02-xds/README.md)。先保存 `curl http://127.0.0.1:18080/` 的 blue 响应，确认当前路由。然后执行：

```sh
curl -fsS -X POST 'http://127.0.0.1:19000/switch?backend=green'
curl -fsS http://127.0.0.1:18080/
curl -fsS -X POST 'http://127.0.0.1:19000/switch?backend=blue'
curl -fsS http://127.0.0.1:18080/
```

管理接口负责向缓存发布候选配置，请求可能在配置传播过程中短暂看到旧结果；实验 smoke 脚本会等待目标行为。记录每次版本变化，并检查两个 Cluster/EDS 都存在，仅 RDS 目标切换。解释 `Node.ID=envoy-book` 如何关联缓存组、`Consistent()` 检查哪些依赖，以及为什么它不能证明业务响应正确。

主动提交 `backend=purple`，预期接口拒绝、当前有效版本与路由不因非法输入而改变。再把 Node.ID 临时改错（仅实验副本），观察连接正常与配置可用不是同一件事。恢复后重新验证。不要把“不返回响应”当成可接受的错误处理：测试应该有超时，失败应该留下可判断的证据。

## 阶段三：Gateway API

先运行[实验三](../../labs/03-envoy-gateway/README.md)，验证 book.example 的基本入口。为了练习蓝绿，在现有独立集群里新增一个 green Deployment 与 Service，可以沿用实验 echo 镜像并用不同的服务/Deployment 名字，让响应中的实例身份明确可区分。还必须把 green Deployment.spec.selector.matchLabels、Pod template.metadata.labels 和 green Service.spec.selector 一起改成独立标签，例如 `app: book-green-backend`；只改对象名字会使两个 Service 可能同时选中蓝绿两套 Pod。保留原 backend 作为 blue。等待 green Deployment rollout 完成，并核对 green Service 的 EndpointSlice 只含 green Pod，再修改路由。不要修改 EG 自动生成的 Envoy Deployment 来表达应用路由。

修改 HTTPRoute.backendRefs 指向 green Service 及其 Service.port。每次提交后检查该 parent 的 Accepted、ResolvedRefs 和 observedGeneration，再检查请求响应。切回原 backend，重复同样证据流程。若想练习加权分流，在两个 backendRefs 设置显式 weight，并使用足够多独立请求观察统计倾向；少量请求不保证严格比例。

这一步需要你自己填写真正的 green manifest 和切换 patch。先回顾第 16–19 章，再在笔记中说明：哪些资源由你声明、哪些由 EG 生成、哪些 Kubernetes 资源提供后端地址。以 Service.port 连接 HTTPRoute，以 targetPort 连接 Pod 监听，不能把它们任意互换。

## 做一张映射表

| 意图 | 静态 Envoy | Go 控制平面 | Gateway API / EG |
| --- | --- | --- | --- |
| 对外入口 | Listener/HCM | LDS 资源 | Gateway listener |
| 域名与路径 | VirtualHost/Route | RDS 资源 | HTTPRoute hostnames/matches |
| 目标服务 | Cluster | CDS 与 EDS | backendRefs、Service、EndpointSlice |
| 发布变化 | 文件加载/重启 | SnapshotCache 与 ADS | API 更新与控制器协调 |
| 确认结果 | Admin 与请求 | 发布日志、ACK、Admin、请求 | status、数据平面、请求 |

映射是学习用近似，一个 Gateway API 对象可能展开为多份 Envoy 资源，资源名也可能经过生成和规范化。不要依赖自动生成名字的某个字符串格式作为应用业务接口。

## 验收与参考答案

请回答五个口头问题：业务请求经过 Go 服务吗？管理接口返回成功证明什么？EG 的 Accepted 与 ResolvedRefs 为何分开？入口 TLS 是否覆盖后端连接？坏配置与坏后端分别怎么查？

<details><summary>参考答案与自评标准</summary>

业务请求经 Envoy 到后端，Go 服务处理配置流。管理接口通常证明发布或受理完成，还需确认 Envoy 激活和业务行为。Accepted 表达接入/接受条件，ResolvedRefs 表达引用解析条件，两者不能合并成“流量必通”。下游和上游 TLS 是独立连接。坏配置从意图、翻译、版本与 NACK 查；坏后端则从 Cluster、端点、健康、连接和应用响应查。

若能准确画出组件、执行三种切换、解释非法输入为什么不会改变有效配置、区分至少两层故障并恢复，说明已经达到本书入门目标。若只能复述命令，请回到资源图和实际 dump 逐条标注。

</details>

实验结束按各 README 清理独立资源。保留笔记和声明文件，它们才是可以重新构建实验的项目交付物。
