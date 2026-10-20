# 第 10 章　把一次转发拆成资源图

本章承接[第 08 章](08-xds-protocol.md)的发现服务和[第 09 章](09-go-and-protobuf.md)的消息类型。我们不急着写一个完整服务器，而是先为一条真实请求建立资源模型。读完后，你应该能手工画出 Listener、Route、Cluster、Endpoint 之间的引用，知道名称在哪里必须相等，并能判断某个信息应该内嵌还是单独发现。

设定一个贯穿本章的场景：入口监听 `10000` 端口，接收 `shop.example` 的 HTTP 请求，把 `/orders` 转给逻辑后端 `orders`，后端实例地址是 `127.0.0.1:18081`。这里的地址仅用于说明；容器中的回环地址属于当前容器，是否能连接宿主机或另一容器，要根据实际网络检查。

## 10.1 从连接层开始画图

Listener 不是 HTTP 路由表。它定义接收连接的地址和过滤器链。对于普通 HTTP 代理，网络过滤器链中放 HTTP Connection Manager，简称 HCM。HCM 负责把连接上的字节识别为 HTTP，建立 HTTP 处理流程，再依次调用 HTTP 过滤器。最后常见的 router 过滤器按照选好的路由执行转发。

因此配置里可能看到两层“filters”：Listener 的网络过滤器，以及 HCM 内部的 HTTP 过滤器。把 `envoy.filters.http.router` 直接塞进网络过滤器列表，并不会得到同样的含义。它们的扩展接口和配置消息都不同。这也是 Any 的类型 URL 必须与所在扩展槽位匹配的原因。

HCM 可以把整个 RouteConfiguration 内嵌，也可以通过 RDS 按名称加载。我们采用第二种，使路由能够单独更新：

```text
Listener: public-http
  └─ HCM: rds.route_config_name = shop-routes
       └─ RouteConfiguration: shop-routes
            └─ VirtualHost: shop，domains = [shop.example]
                 └─ prefix /orders → Cluster: orders
                                      └─ EDS → ClusterLoadAssignment: orders
                                                └─ 127.0.0.1:18081
```

注意，VirtualHost 名字 `shop` 主要用于标识和管理；真正匹配请求 Host 的是 `domains`。RouteConfiguration 的名字 `shop-routes` 则必须与 HCM 的 `route_config_name` 对应。名称不是随意写的注释，而是控制平面与代理共同使用的连接点。

## 10.2 Cluster 描述服务，Endpoint 描述地址

Cluster 定义逻辑后端的连接超时、发现方式、负载均衡、TLS 和协议相关选项。它不一定只代表一个地址。本例选择 EDS，让实例地址通过独立资源提供。构造的核心可以写成：

```go
orders := &clusterpb.Cluster{
    Name: "orders",
    ConnectTimeout: durationpb.New(3 * time.Second),
    ClusterDiscoveryType: &clusterpb.Cluster_Type{
        Type: clusterpb.Cluster_EDS,
    },
    EdsClusterConfig: &clusterpb.Cluster_EdsClusterConfig{
        EdsConfig: &corepb.ConfigSource{
            ResourceApiVersion: corepb.ApiVersion_V3,
            ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
                Ads: &corepb.AggregatedConfigSource{},
            },
        },
    },
}
```

这是函数内部的资源构造片段，`clusterpb`、`corepb` 是相应的 v3 消息包，`durationpb` 负责时间编码。片段表达两个独立选择：Cluster 采用 EDS；EDS 消息从 ADS 配置源获取。只把发现类型改为 EDS、却不给 EDS 任何配置源，是不完整的选择。

EDS 的核心资源名有一个特殊点：`ClusterLoadAssignment` 用 `cluster_name` 标识自己，而不是普通的 `name` 字段。库的 `GetResourceName` 为此做了特殊分支。默认情况下，Cluster 名 `orders` 对应 EDS 资源名 `orders`；若 Cluster 的 `eds_cluster_config.service_name` 设置成 `orders-endpoints`，则应提供 `cluster_name=orders-endpoints` 的资源。路由仍然转发到 Cluster `orders`，不会跟着改成 EDS 的名称。

地址下面有多层结构：ClusterLoadAssignment 包含 locality 分组，每组包含 LbEndpoint，后者才指向具体 Endpoint 和 socket 地址。这样的层次允许按地域、优先级和权重组织实例。入门阶段可以只有一个 locality、一个地址，但不应把外层结构当成没有意义的包装；它们决定了以后怎样表达同城优先和跨地域故障转移。

## 10.3 RouteConfiguration 不是一条 Route

RouteConfiguration 容纳若干 VirtualHost；VirtualHost 按域名选择；其 Routes 列表再按路径、请求头等条件选择。一个典型 Route 的概念形状如下：

```yaml
name: shop-routes
virtual_hosts:
- name: shop
  domains: [shop.example]
  routes:
  - match:
      prefix: /orders
    route:
      cluster: orders
```

这是消息的 YAML 阅读形式，不是本书实验额外支持的输入格式。它帮助你把 Go 中层层结构体还原成一个业务规则。`route.cluster` 引用 CDS 中的 Cluster 名；它不是 URL，也不是 Kubernetes Service 名的自动解析入口。你可以在应用翻译层约定二者一致，但 Envoy 只认识收到的配置。

路由匹配还需要考虑顺序。若在精确业务规则前放一个匹配 `/` 的兜底，后面的规则可能永远没有机会被选中。`prefix: /orders` 还可能匹配以相同字符串开头的其他路径；若需求是严格的路径段边界，应选择匹配语义合适的字段并验证边界请求。控制平面编译成功，不能证明业务规则表达正确。

本章选择转发动作，Route 还可以直接响应或重定向。直接响应不需要后端 Cluster，因此一个合法的 HTTP Listener 和 RouteConfiguration 可以完全没有 CDS/EDS 业务资源。后面实验的具体实现若使用直接响应，应按它的资源图阅读，不要硬找一个并不存在的后端。

## 10.4 配置源与 bootstrap 解决先有谁的问题

代理必须先知道怎样找到控制平面，才能获取动态配置。这些最初的连接信息放在 bootstrap：通常包括 Node 信息、用于 xDS 连接的静态 Cluster，以及 `dynamic_resources` 中的 ADS 配置。业务 Cluster 和控制平面连接 Cluster 用途不同；若连寻找控制平面的能力本身都只能从尚未连接的控制平面获取，就出现循环依赖。

在使用 ADS 时，bootstrap 指明 ADS gRPC 服务的位置；动态 Listener 中的 RDS、动态 Cluster 中的 EDS 再以 ADS 作为配置源。只在 bootstrap 开了 ADS，而资源内部还指向一个不存在的独立 gRPC Cluster，代理仍然可能收不到依赖。排查时应同时读“资源引用哪个名字”和“这个名字从哪种配置源取得”。

静态与动态也可以组合。bootstrap 静态 Listener 使用动态 RDS，是一种有效布局；动态 CDS 使用 DNS Cluster，也是一种有效布局。不存在“启用 xDS 就必须把所有字段都搬进同一种发现服务”的规则。明确各部分的责任，比追求全部动态更有帮助。

## 10.5 SDS 增加了资源，也增加了权限边界

当入口采用 HTTPS，Listener 的 TLS transport socket 可以通过 SDS 引用证书 Secret；后端 TLS 也可能引用验证材料。SDS 支持动态提供这些信息，但它不自动解决密钥从哪里安全取得、谁有权订阅哪个 Secret、证书何时续期等问题。TLS 内容还涉及有效期、域名、信任根和证书私钥匹配。

资源图里有证书引用，不代表快照检查已经验证了它。`v0.14.0` 的 `Snapshot.Consistent()` 主要核对 EDS 和 RDS 的特定引用集合，并没有遍历和验证整个 TLS 图。因此“快照一致”与“HTTPS 可用”之间还差着类型校验、代理校验以及真实握手。私钥也不应该为了调试而出现在公开的书籍、日志或仓库里；示例应使用明确的测试材料。

## 10.6 名称和类型的三类错误

第一类是引用拼写错：HCM 要 `shop-routes`，缓存里只有 `shop-route`。检查引用关系可以较早发现它。第二类是同类型重名：两个 RouteConfiguration 都叫 `shop-routes`。库把切片转换成按名 map 时，后一个会覆盖前一个，不会替你生成第二个名字。应用应在建快照之前显式拒绝重复。

第三类是类型放错：在 `resource.ClusterType` 键下放了一条 Listener。通用 `types.Resource` 只保证它是 Protobuf 消息，`NewSnapshot` 对键是否为已知类型有检查，却不等于已经检查每个值与键的语义匹配。这说明类型化 Go API 降低了错误概率，但没有消除发布前验证的责任。

## 10.7 练习与参考答案

1. Cluster 名是 `orders`，EDS `service_name` 是 `orders-v2-addresses`。Route 的 cluster 与 ClusterLoadAssignment 的 cluster_name 各应是什么？
2. 静态 DNS Cluster 一定需要独立 EDS 资源吗？
3. 一个直接返回固定文本的 HTTP Route 是否必须包含后端 Cluster？

参考答案：第一题分别是 `orders` 和 `orders-v2-addresses`，两者处在不同引用关系上。第二题不需要，发现方式决定地址来源。第三题不需要，直接响应由代理本身完成。练习时可对照[实验 02](../../labs/02-xds/README.md)实际构造的对象，把自己的图与源码逐一对应。

实验的实际资源名是 `listener_0`、`book_routes`、`blue_backend` 和 `green_backend`，与本章讲解用的 shop/orders 名称不同。blue、green 容器通过 Compose 的 `network_mode: service:envoy` 与 Envoy 共用网络命名空间，所以 EDS 的数值地址 `127.0.0.1:8081` 和 `127.0.0.1:8082` 可以访问它们。这是刻意设计的本地网络布局；普通的两个独立容器不会因为使用相同回环地址就互通，真实 EDS 应发布从代理所在网络可达的端点 IP。

## 本章源码

- [资源命名与引用提取](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/resource.go)：读 `GetResourceName`、`getClusterReferences`、`getListenerReferences`。
- [上游 EDS、Route 和 Listener 构造工具](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/test/resource/v3/resource.go)：它们是测试辅助，不是平台自动翻译器。
- [较小的完整资源示例](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/internal/example/resource.go)：注意该例使用 DNS Cluster，与本章 EDS 分解不同。
- [TLS Secret 测试资源](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/test/resource/v3/secret.go)与[Secret 生成类型](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/envoy/extensions/transport_sockets/tls/v3/secret.pb.go)。
