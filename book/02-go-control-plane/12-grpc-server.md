# 第 12 章　把 cache 接到真正的 gRPC 服务器

上一章的 SnapshotCache 只是进程中的对象。只有把它接到协议处理器，再把处理器注册到监听端口，Envoy 才能通过网络订阅。本章需要你能读懂基本 Go 函数与 context，不要求熟悉所有 gRPC 细节。目标是认清四个对象的职责，知道何时注册 ADS，能够从日志分辨“流建立”“响应准备发送”和“代理接受配置”。

我们也会接上[实验 02](../../labs/02-xds/README.md)：宿主机 `18080` 是业务入口，`19000` 是实验控制接口，`19901` 是 Envoy admin；容器网络里的 `control-plane:18000` 才是 ADS 服务地址。不同端口对应不同角色，不能用对一个 HTTP 业务端口的测试替代对 xDS 连接的检查。

## 12.1 四个对象依次承担什么

第一个对象是 cache，保存每组代理应该得到的资源。第二个对象是 `server.NewServer` 返回的 xDS handler，负责处理 DiscoveryRequest、维护流与 watch、生成 nonce，并从 cache 取响应。第三个对象是 `grpc.NewServer` 返回的 gRPC server，负责接受 RPC 调用并把调用交给注册的服务。第四个对象是 `net.Listener`，负责在地址和端口上接受网络连接。

```text
Envoy ──gRPC/HTTP2──► net.Listener
                         │
                    grpc.Server
                         │ 服务注册
                    xDS handler
                         │ watch 与响应
                    SnapshotCache ◄── 应用发布新配置
```

`NewServer` 不会自己打开端口；`NewSnapshotCache(true, ...)` 中的 true 也不会帮你注册 ADS。漏掉注册时，TCP 连接可能成功，但调用得到 gRPC `Unimplemented`；监听地址错误时则可能连 TCP 都无法建立。这两个问题需要完全不同的排查路径。

## 12.2 最小接线代码

下面是一个可独立阅读的函数，使用上游 `v0.14.0` 的 API。调用者负责创建和填充 cache、创建监听器，并决定进程何时取消 ctx。为了突出连接关系，关闭使用立即停止；完整程序还应按自己的服务生命周期安排优雅退出。

```go
package xdsserve

import (
    "context"
    "net"

    discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
    cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
    sotw "github.com/envoyproxy/go-control-plane/pkg/server/sotw/v3"
    serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
    "google.golang.org/grpc"
)

func Serve(ctx context.Context, lis net.Listener, config cachev3.Cache) error {
    xds := serverv3.NewServer(ctx, config, nil, sotw.WithOrderedADS())
    gs := grpc.NewServer()
    discoverygrpc.RegisterAggregatedDiscoveryServiceServer(gs, xds)

    done := make(chan struct{})
    go func() {
        select {
        case <-ctx.Done():
            gs.Stop()
        case <-done:
        }
    }()
    err := gs.Serve(lis)
    close(done)
    return err
}
```

`cachev3.Cache` 是服务器需要的 cache 接口，SnapshotCache 实现了它。`RegisterAggregatedDiscoveryServiceServer` 把 ADS 服务登记到这个 gRPC server。只通过 ADS 获取这些资源的客户端，可以由这项注册服务；若客户端使用独立 CDS、EDS、LDS 等 RPC，还要分别调用对应生成包的注册函数。对象实现了方法和网络上已经暴露该服务，是两件事。

`WithOrderedADS()` 是 SotW 服务器选项，选择有序 ADS 处理路径。cache 中 ADS 标志与服务器选项负责不同环节：前者影响资源集合等待和 cache 的通知顺序，后者影响流上的响应处理。它们有助于依赖收敛，却不会把跨类型更新变成事务，不能据此省去发布顺序设计。

这段代码没有设置 TLS，适合在本地隔离实验里理解协议。生产连接的认证与授权放在[第 15 章](15-production-boundaries.md)讨论。不要从“gRPC 默认能跑”推导出“这个端口已经适合暴露给任意客户端”。

## 12.3 context 要跟对生命周期

程序通常有一个在收到退出信号后取消的进程 context。xDS server 需要这样的长期 context，让所有流在关闭时结束。每条 gRPC 流还有自己的 context，客户端断开或流被取消时它会结束；单次管理请求又有自己的 context，可能带几秒钟的超时。

这三者不应混用。例如管理 HTTP 请求调用一次发布后已经返回，如果你恰好把该请求的 context 当成 xDS server 的生命周期，后续代理可能莫名其妙断流。同样，设置 `defer cancel()` 的局部函数若只负责初始化，却把它创建的 context 交给长期运行的服务器，函数一返回就会取消服务。

context 的取消是协作机制。库会在相应循环里观察取消信号，但其他业务 goroutine、数据库操作、消息队列都需要各自传递和响应它。停止 gRPC 时，`GracefulStop` 会等待活动 RPC 结束；xDS 是长期流，如果客户端一直保持连接，等待可能很久。实际服务通常先撤销就绪状态、安排排空，给优雅退出一个期限，超时再 `Stop`。期限不能由一个永不结束的后台等待代替。

一次 `SetSnapshot` 的 context 主要影响本次通知等待。其错误不代表缓存自动回滚，原因见[第 11 章](11-snapshot-cache.md)。生命周期设计和发布状态设计必须分别明确。

## 12.4 回调让协议状态变得可见

`server.CallbackFuncs` 让应用只提供关心的函数，不必手写整个回调接口。例如在 `StreamRequestFunc` 里记录收到的 Node、type URL、version、response nonce 和错误；在 `StreamResponseFunc` 里记录准备发送的版本、nonce 和资源名称。上游声明这些回调是同步调用，慢回调会阻塞相应协议处理。

需要强调三个时序。`OnStreamOpen` 发生在流打开时，尚不能假定已经有第一条请求的 Node；认证可从 transport context 获取，Node 相关授权则还要在请求阶段核对。`OnStreamResponse` 发生在发送之前，所以它不能证明网络发送完成。真正的 ACK/NACK 必须从之后的客户端请求中识别，并与该流、该类型之前发出的 nonce 关联。

不能把所有没有 `error_detail` 的请求都统计成 ACK：首次订阅也通常没有错误。相同请求还可能改变订阅集合。更可靠的观察记录至少包含 stream ID、Node 或组键、资源类型、已发送版本与 nonce、确认版本、错误详情和时间。nonce 应按流关联，不能把不同连接的相同数字拼在一起。

日志也需要边界。SDS 消息可能带密钥，直接打印整个 Protobuf 对象会泄露敏感信息；Node.ID 若逐个作为指标标签，规模变大时会造成高基数。日志可保留排查关联信息，指标则优先按类型、结果和有限组别聚合。异步写审计时要有队列上限，不能用无界 goroutine 掩盖慢存储。

## 12.5 在实验中验证“接线”

从仓库根目录进入实验，再启动：

```sh
cd labs/02-xds
docker compose up --build -d
docker compose ps
curl -fsS http://127.0.0.1:18080/
```

初始业务响应应来自 blue 后端。命令返回后，服务可能还在启动；若第一次连接失败，先查看 `docker compose ps` 和 `docker compose logs`，确认镜像构建、端口绑定以及代理订阅过程。不要把“容器 Running”直接当成代理已经获得完整配置。

实验 Node.ID 固定为 `envoy-book`，应用发布缓存键需要与 `IDHash` 的结果对应。若把 bootstrap 改成另一个 ID，而发布仍留在旧键，xDS 流可以存在，但客户端会等待属于自己的 snapshot。另一个常见错误是把宿主机 `127.0.0.1:18000` 当作容器内控制平面地址；Compose 服务之间应使用服务名和容器端口，回环地址指的是当前容器自己。

普通 curl 请求也不能直接当作 ADS 客户端。ADS 使用 gRPC 双向流，包含按协议编码的消息；向 `18000` 发普通 HTTP GET，即使得到了某个错误，也不足以判断 xDS 消息处理是否正确。应结合代理日志、控制平面回调和 admin 中的资源状态验证。

## 12.6 练习与参考答案

1. cache 已创建、snapshot 已发布，为什么代理仍可能得到 `Unimplemented`？
2. 响应回调打印 `version=17`，是否能把发布状态记为“Envoy 已接受”？
3. 为什么只调用 `GracefulStop()` 可能让退出一直等待？

参考答案：第一题需要检查所调用的 ADS 或独立 xDS 服务是否已在当前 grpc.Server 上注册，以及代理是否连到了正确实例。第二题不能，回调发生在发送前，需等待与 nonce 对应的客户端 ACK，并继续验证运行状态。第三题 xDS 是长流，活动 RPC 可能不自行结束，应结合 context 取消、排空和有界强制停止。

## 本章源码

- [组合 xDS 服务器与 CallbackFuncs](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/v3/server.go)：看 `NewServer` 如何组合 REST、SotW 和 Delta。
- [SotW 流、发送与回调时序](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/sotw/v3/server.go)、[有序 ADS 路径](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/sotw/v3/ads.go)。
- [上游 gRPC 服务注册与监听示例](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/internal/example/server.go)、[测试服务注册](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/test/v3/register.go)。
- [ADS 生成的 gRPC 接口与注册函数](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/envoy/service/discovery/v3/ads_grpc.pb.go)。
