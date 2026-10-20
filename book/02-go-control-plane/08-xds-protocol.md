# 第 08 章　xDS：控制平面与代理怎样对话

到这一章，我们把视角从“用户声明希望怎样转发流量”，移到“代理收到什么配置”。你不需要先会写 Go；只要知道客户端向服务器发送请求，服务器返回消息，就能开始。读完本章，应当能解释一条 xDS 流里的订阅、响应、ACK 与 NACK，能够分辨配置版本和报文编号，并且知道为什么 ADS 不能保证一次修改中的所有资源同时生效。

本篇以 `envoyproxy/go-control-plane` 的 `v0.14.0` 为源码基线。这里的 xDS 是一组配置发现协议，go-control-plane 则是实现这些协议的 Go 库。协议、库、用库构建的控制平面，是三个层次。启动库的服务器，并不会自动发现 Kubernetes Service，更不会自动把 HTTPRoute 翻译成路由。

## 8.1 从一条普通 HTTP 请求反推配置

假设浏览器访问 `http://shop.example/orders`。Envoy 首先要知道在哪个端口接收连接；接着要识别 HTTP，按域名和路径选择路由；最后要选择后端服务及其某个地址。如果入口使用 HTTPS，还要取得证书与私钥。xDS 把这些信息拆成不同资源，而不是每次都发送一份巨大的配置文件。

| 发现服务 | 主要资源 | 回答的问题 |
| --- | --- | --- |
| LDS | Listener | 在哪里接收连接，使用哪些网络过滤器？ |
| RDS | RouteConfiguration | HTTP 域名、路径、请求头如何选择动作？ |
| CDS | Cluster | 后端服务采用什么连接、发现和负载均衡方式？ |
| EDS | ClusterLoadAssignment | 这个后端服务现在有哪些可用地址？ |
| SDS | Secret | TLS 所需证书、私钥或验证材料是什么？ |

Cluster 可以理解为一个逻辑后端池，不要把它与 Kubernetes 集群混为一谈。EDS 也不直接等于健康检查：它主要提供地址及相关属性，运行中的连接是否成功还受网络和健康状态影响。SDS 的 Secret 则是 Envoy API 资源，不能直接当作 Kubernetes Secret 对象发送。

这些服务不是所有场景都必需。例如，Cluster 可以通过 DNS 找到后端，也可以把地址内嵌在配置中，此时未必另用 EDS；HTTP 路由也可以内嵌在 Listener 的 HTTP Connection Manager 中，此时未必另用 RDS。拆分的意义是让变化可以独立传播，代价是资源之间出现名称引用与更新时间差。

## 8.2 订阅是一段持续的对话

在流式 gRPC 模式下，Envoy 主动连接控制平面，发送自己是谁、需要什么类型和哪些名称的资源。控制平面可以先回答，也可以暂时等到有新配置再回答。这不是 Envoy 暴露一个端口等待控制平面主动拨入，也不是每个业务请求都来控制平面查询一次。

下面用接近 JSON 的方式展示字段；这是帮助阅读的协议示意，不是可直接交给实验程序的命令：

```json
{
  "node": {"id": "edge-a"},
  "type_url": "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
  "resource_names": ["shop-routes"],
  "version_info": "",
  "response_nonce": ""
}
```

`node.id` 标识代理。第一次请求中的版本为空，表示客户端尚未成功接受这个类型的配置。`type_url` 说明接下来应该把资源当成哪一种 Protobuf 消息解码；`resource_names` 说明要订阅哪些资源。在 ADS 里多个类型共用一条流，因此类型字段尤其关键。CDS、LDS 常使用通配订阅，EDS、RDS 常由已有资源中的引用触发按名称订阅；空列表的精确含义还与协议模式、客户端及库的通配设置有关，不能把所有空列表都解释成“删除所有资源”。

控制平面随后返回 `version_info: "r17"`、`nonce: "42"` 和资源内容。这里的 `r17` 是该类型配置的版本标识，`42` 是本次响应的关联标识。版本字符串是不可解释的值，客户端主要比较是否相等；`r10` 不天然比 `r9` 更新。nonce 用来回答“你在确认哪一次响应”，不承担内容版本的职责，也不应该当作跨连接的永久序号。

## 8.3 ACK、NACK 与最后一次可用配置

如果 Envoy 成功处理版本 `r17`，它会发回同类型请求，带上 `version_info: "r17"`、`response_nonce: "42"`，且没有错误详情，这就是 ACK。如果新版本 `r18` 的响应 nonce 是 `43`，但内容不合法，典型 NACK 会带 `response_nonce: "43"`、仍为 `r17` 的成功版本，以及 `error_detail`。因此“请求里带了旧版本”不能单独证明网络故障，要结合 nonce、错误和之前发送的内容判断。

```text
控制平面                         Envoy
  r17，nonce=42  ───────────────► 校验并接受
                 ◄───────────── ACK：r17，nonce=42
  r18，nonce=43  ───────────────► 校验失败
                 ◄───────────── NACK：r17，nonce=43，error_detail
```

在通常的拒绝更新场景里，Envoy 继续使用最后成功接受的配置，也就是 Last Known Good，简称 LKG。但是这句话不等于“整个代理自动回滚到同一个全局版本”。每个资源类型有自己的状态，某个 Cluster 的变化可能已被接受，而另一个 Listener 的变化被拒绝。初次启动没有 LKG 时，拒绝配置更不可能凭空产生可服务的入口；采用 TTL 的资源也有过期删除语义。

ACK 是配置处理结果，不是业务探测成功证明。后端可以在 ACK 后掉线；端口、防火墙和证书链的实际行为也需要单独验证。服务器的“发送响应”日志同样不是 ACK 日志，因为响应回调发生在真正发送之前。后面的[第 12 章](12-grpc-server.md)会沿源码说明这个时序。

## 8.4 SotW 与 Delta 解决的是更新粒度

State of the World，简称 SotW，使用 `DiscoveryRequest` 和 `DiscoveryResponse`，以资源类型的版本来描述状态。需要避免一个常见误读：SotW 并不是“任何响应都包含整个代理的全部配置”。不同类型依然分开发送；CDS、LDS 的响应对删除有完整集合语义，按名称请求的类型则有各自规则。不能把某次 RDS 响应没出现一个名称，机械解释成与 CDS 完全相同的删除操作。

Delta 使用 `DeltaDiscoveryRequest` 和 `DeltaDiscoveryResponse`。客户端用 subscribe、unsubscribe 增减订阅；服务器给每个资源单独的版本，并通过 `removed_resources` 明确报告删除。未出现在本次增量中的资源通常表示没有变化，而非被删除。重新建立流时，客户端可以在每个类型首次请求里报告 `initial_resource_versions`，让服务器知道它还保留哪些资源。

Delta 响应里的 `system_version_info` 主要用于调试，不能代替每个资源的版本。Delta 也没有消灭 ACK、NACK、资源依赖或安全校验，只是减少某些变化必须重传的内容。对刚入门的实现，先理解 SotW SnapshotCache，再研究大量端点频繁变化时是否值得使用 Delta，更容易定位问题。

## 8.5 ADS 聚合连接，但不创造跨类型事务

ADS 把 CDS、EDS、LDS、RDS 等消息放在一条双向流上，仍用 `type_url` 区分各自状态。它有助于控制发送顺序、协调依赖，并减少管理多条连接的复杂度。它不提供“所有代理同时接受全部资源，否则全部撤销”的提交协议。一条流有顺序，也不等于代理上的所有运行状态在同一时刻切换。

例如把路由从 `orders-old` 改到 `orders-new`，比较稳妥的发布是先提供新 Cluster 及地址，确认它可用，再改变 Route，最后清理旧 Cluster。若直接先删旧后端再改路由，代理可能短时间找不到路由引用的目标。即使把所有对象放进一次 snapshot，仍要考虑代理的订阅、校验、warming 和不同连接上的收敛速度。[第 13 章](13-dynamic-updates.md)会把这个过程扩展成可观察的发布流程。

## 8.6 练习与参考答案

1. 已接受的 RDS 版本是 `a`。服务器发送 `b`、nonce `7`，Envoy 拒绝了它。NACK 的三个关键字段是什么？
2. ADS 流中 CDS 被 ACK，RDS 被 NACK，能否说这次全局发布成功？
3. Delta 响应只含端点集合 `orders`，是否表示客户端原有 `payments` 必须删除？

参考答案：第一题是 `version_info=a`、`response_nonce=7` 和非空错误详情；nonce 指向被拒绝的响应，版本保留最近成功值。第二题不能：不同类型独立确认，必须继续检查依赖和业务结果。第三题不能：要查看是否存在针对 `payments` 的明确删除信息，并结合订阅状态理解。

## 本章源码与进一步阅读

- [DiscoveryRequest、DiscoveryResponse 与 Delta 消息定义](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/envoy/service/discovery/v3/discovery.pb.go)：优先读字段注释，再看生成方法。
- [xDS 类型 URL 常量](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/resource/v3/resource.go)：对照表格找到准确资源名。
- [SotW nonce 生成与发送回调](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/sotw/v3/server.go)及[请求 nonce 检查](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/sotw/v3/xds.go)。
- [资源类型的 SotW 完整状态规则](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/resource.go)与[Delta 差异计算](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/delta.go)。
- [Envoy 官方 xDS 协议说明](https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol)：该链接跟随最新文档，实际兼容性仍应对照部署的 Envoy 版本。
