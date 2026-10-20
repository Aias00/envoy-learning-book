# 05　逐段读懂第一份静态配置

本章目标：能读懂一份小型 Envoy YAML，解释监听地址、HCM、路由和 Cluster 的作用，并知道 Admin 接口与业务入口的区别。先修：第 04 章资源图。完整可运行配置以 [实验一](../../labs/01-static-envoy/README.md) 为准，下面分段解释常用写法；这些是便于阅读的片段，请勿把它们单独当完整启动文件。

## 文件只是消息的一种表示

Envoy 的配置模型来自 Protobuf。YAML 中的字段最终映射到这些有类型的消息，因此缩进正确仅是第一步，字段名、枚举、扩展类型和资源引用也要有效。看到 `@type` 时，不必以为它是魔法标记：它标明当前 typed_config 中装的是哪种 Protobuf 消息。第 09 章会用 Go 的 Any 类型重现这个过程。

最外层 `static_resources` 包含启动时已知的 Listener 和 Cluster。片段如下：

```yaml
static_resources:
  listeners:
    - name: listener_0
      address:
        socket_address:
          address: 0.0.0.0
          port_value: 8080
      filter_chains:
        - filters:
            - name: envoy.filters.network.http_connection_manager
              typed_config:
                '@type': type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
                stat_prefix: ingress_http
```

`0.0.0.0` 表示在当前网络环境监听所有 IPv4 接口；不是发给客户端的目标地址。`8080` 是容器内端口。Compose 可以把它映射到宿主机 `127.0.0.1:18000`，因此你在终端访问的是 18000 而配置文件写的是 8080，这两个数字可以不同。

HCM 的 `stat_prefix` 是指标名字的一部分，便于区分不同 HTTP 连接管理器；它不是路由前缀。一个配置中出现多个相似字符串时，请先确认它是对象名字、地址、匹配条件还是观测标签。

## 给 HCM 添加路由和 router

以下内容应位于同一个 HCM `typed_config` 下：

```yaml
route_config:
  name: local_routes
  virtual_hosts:
    - name: all_hosts
      domains: ['*']
      routes:
        - match:
            prefix: /green
          route:
            cluster: green_backend
        - match:
            prefix: /
          route:
            cluster: blue_backend
http_filters:
  - name: envoy.filters.http.router
    typed_config:
      '@type': type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

`domains: ['*']` 让这个入门配置接受任意 Host；真实多租户入口应使用明确域名和授权。`/green` 前缀先选择 green，其余请求落到 `/` 兜底规则。多条 Envoy 路由按顺序尝试，前面过于宽泛的匹配可能遮挡后面的具体规则，所以不要随便把兜底规则放在首位。Gateway API 有自己的匹配优先规则，不能把这条“Envoy 路由顺序”机械套用到 Kubernetes 资源创建顺序。

`route.cluster` 指向上游 Cluster。router 是终结型 HTTP 过滤器，通常放在 HTTP 过滤器链末尾，让前面的认证、改头等过滤器有机会先运行。即使已经写了路由，漏掉实际转发过滤器也不等于请求会自动离开 Envoy。

## Cluster 把名字接到网络地址

以下与 `listeners` 同属于 `static_resources`：

```yaml
clusters:
  - name: blue_backend
    connect_timeout: 1s
    type: STRICT_DNS
    dns_lookup_family: V4_ONLY
    lb_policy: ROUND_ROBIN
    load_assignment:
      cluster_name: blue_backend
      endpoints:
        - lb_endpoints:
            - endpoint:
                address:
                  socket_address:
                    address: blue
                    port_value: 8080
```

`connect_timeout` 限制到后端建连；路由也可显式设置 `timeout`，管理另一种时间预算，完整实验文件未覆盖其默认值。ROUND_ROBIN 是从可用主机中轮询选择，不负责把 `/products` 变成商品服务，后者是路由职责。`blue` 是 Compose 网络中的服务名，8080 是该服务的内部端口。宿主机端口映射不能直接代替容器间地址。V4_ONLY 配合本实验只监听 IPv4 的后端，避免双栈 DNS 优先选到不可达 IPv6 地址。

`load_assignment.endpoints` 的分层还允许表达 locality 等信息；初学时先理解为“上游端点列表”，读 EDS 时再扩展。名字中有 endpoint，不意味着每一层对象都是单一 IP。

## Admin 是观察与操作入口

Admin 接口可以查看 `/server_info`、`/stats`、`/config_dump` 和 `/clusters` 等信息。它也有操作性接口，且配置内容可能含敏感数据，所以应限制访问。实验里宿主机只绑定 loopback，用浏览器或 curl 本地观察。不要把 Admin 映射成公共网站入口。

配置在容器里监听 `0.0.0.0` 以便 Docker 端口映射访问，与宿主机发布 `127.0.0.1:19900` 不矛盾。前者是容器内监听范围，后者限制宿主机暴露范围。若你把发布地址改成所有网卡，本书的本机访问边界也跟着改变。

## 练习与参考答案

1. 改 `stat_prefix` 能改变 `/api` 的路由吗？
2. 在 Cluster 的 address 写宿主机 18000 端口，能访问后端吗？
3. 能否只用 YAML 解析器验证整份 Envoy 配置？

<details><summary>参考答案</summary>

1. 不能，它主要影响指标命名前缀；应修改路由匹配和 action。
2. 一般不行，那是本实验的 Envoy 入口映射。容器间应使用后端服务名及其内部监听端口。
3. 不能。还应使用对应版本 Envoy 验证配置，再运行请求验证资源和网络语义。YAML 语法正确并不保证消息字段或资源引用正确。

</details>

继续：[运行实验](06-first-lab.md)。来源：[HCM v3 API](https://www.envoyproxy.io/docs/envoy/v1.36.0/api-v3/extensions/filters/network/http_connection_manager/v3/http_connection_manager.proto)、[Bootstrap v3 API](https://www.envoyproxy.io/docs/envoy/v1.36.0/api-v3/config/bootstrap/v3/bootstrap.proto)、[Admin API](https://www.envoyproxy.io/docs/envoy/v1.36.0/operations/admin)。
