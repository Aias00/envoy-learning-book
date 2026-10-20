# 第 14 章　带着问题走读 go-control-plane 源码

读大型开源仓库时，最容易耗尽耐心的做法是从第一个目录一路往下看。本章换一种方式：用[实验 02](../../labs/02-xds/README.md)的一次订阅和一次发布作为线索，每走到一层只回答一个问题。前置知识是前面各章的资源、cache 和 gRPC 接线。目标是在没有本书陪同的情况下，也能定位 API 定义、理解关键调用关系，并验证自己对库行为的判断。

所有路径都以 `go-control-plane v0.14.0` 为准。本章给的是阅读路线，不要求把上游完整测试系统搬进本书。上游的测试辅助类型、较小示例和对外库代码用途不同；尤其 `internal/example` 受 Go 的 internal 导入规则限制，不是下游项目应该直接依赖的公共 API。

## 14.1 先用入口文件建立地图

从 `internal/example/main/main.go` 开始，能看到创建 cache、构造 snapshot、调用 `Consistent`、`SetSnapshot` 和启动 server 的顺序。先忽略资源里面的几百个字段，把它压缩成一条线：输入产生资源，资源成为 snapshot，snapshot 进入 cache，server 对外提供这些内容。

紧接着读 `internal/example/server.go`，找到 gRPC server 的创建、服务注册和 `Serve`。它说明库没有隐藏一个会自动启动的网络服务。再读 `internal/example/resource.go`，会发现这个例子采用 DNS Cluster 并把地址放在 Cluster 中；这与本书实验的 CDS+EDS 结构不同。因此“示例里没有单独 EndpointType”不代表库不支持 EDS，而是资源发现方式的选择不同。

可以给自己画一张小地图：

```text
internal/example/     示例如何把零件接起来
pkg/resource/v3/      type URL 常量和少量资源辅助函数
pkg/cache/types/      通用资源接口及响应类型枚举
pkg/cache/v3/         快照、cache、watch 与响应构造
pkg/server/v3/        对外的组合 xDS server
pkg/server/sotw/v3/   SotW 流处理与有序 ADS
pkg/server/delta/v3/  Delta 流处理
pkg/server/stream/v3/ 订阅状态和流接口
envoy/               独立模块中的 Envoy 生成消息与 gRPC 接口
```

生成代码可以查字段，但不适合初读时从头看完。看到 `ProtoReflect`、序列化表和巨大描述符时，先记住它们服务于消息运行时；你的问题若是“为什么版本没变化就没有推送”，应回到手写的 cache 与 server。

## 14.2 跟随一条首次 RDS 请求

假设 Envoy 通过 ADS 订阅 `book_routes`。先在 `pkg/server/v3/server.go` 找 `StreamAggregatedResources`，它把调用交给 SotW `StreamHandler`，使用空 type URL 表示由请求自己区分资源类型。独立 RDS 方法则传入 RouteType，类型可以由方法隐含确定。

来到 `pkg/server/sotw/v3/server.go`，`StreamHandler` 通过一个 goroutine 接收 gRPC 请求，再交给处理循环。接收和响应分开，是因为流可能同时有客户端请求和 cache 更新，不能让一个阻塞式 `Recv()` 把所有发送机会挡住。观察退出分支，会看到流 context 与服务器 context 都参与清理。

随后读 `xds.go`。代码保存首条请求带来的 Node，给后续省略 Node 的请求补上；ADS 请求没有 type URL 会被拒绝；请求回调可能返回错误并终止流；已有类型还要检查响应 nonce 是否与最近响应匹配。然后它创建或调整订阅状态，为当前类型调用 cache 的 `CreateWatch`。

如果启用了 `sotw.WithOrderedADS()`，同一入口会转入 `ads.go` 的处理路径。此时多个类型通过一个复用的响应通道发送，以保持相应顺序。不要只修改或阅读默认路径就认为掌握全部 ADS 行为；先确认程序到底传了哪些选项。源码里的选项会改变控制流，这比函数名看起来相似更重要。

## 14.3 在 cache 中回答“为什么还没推送”

进入 `pkg/cache/v3/simple.go` 的 `CreateWatch`，第一步就是通过 `NodeHash` 计算缓存键。若没有对应 snapshot，代码保留 watch 等待；这能解释一个很常见的现场：连接没有报错，但 Node.ID 与发布键不一致，所以永远等不到资源。

已有快照时，代码取出相应类型的版本与资源，再对照请求和订阅。版本相同、客户端也没有新订阅而尚未得到的资源，便继续等待。需要响应时调用 `respond`，其中 ADS 的有名请求还需要符合资源集合条件。由此你可以把“不推送”的排查分为四类：键不匹配、类型不匹配、版本没有变化、订阅集合不符合当前 snapshot 的边界。

`CreateWatch` 的 `cancel` 返回值也值得跟踪。它让 server 在新请求或流关闭时删除旧 watch。若以后自己实现 `ConfigWatcher`，取消必须能释放资源并适应重复调用，且实现需要线程安全。看接口注释比只模仿某个结构体更能避免把生命周期契约遗漏掉。

## 14.4 反向跟随一次发布

再从应用的 `SetSnapshot` 走一遍。`simple.go` 先持锁更换 `snapshots[node]`，再处理该键已有的 SotW 和 Delta watch。SotW 比较新旧版本，生成响应送进通道。ADS 模式还借助 `order.go` 和响应类型枚举排序；Go map 的随机遍历次序不能承担这个任务。

cache 交出的还不是最终带 nonce 的网络响应。`pkg/cache/v3/cache.go` 定义 `Response`、`RawResponse` 以及转换为 DiscoveryResponse 的方法。`cached_resource.go` 延迟序列化资源，并缓存结果或摘要；其中明确要求资源创建后不要再修改。这给前面各章的不可变约定提供了直接源码证据。

回到 SotW 的 `streamWrapper.send`，响应被打包，服务器递增流上的 nonce，更新该类型返回资源记录，调用响应回调，最后执行 gRPC `Send`。因此返回资源记录和响应日志不能直接当作“代理已成功应用”的数据库。代理下一条请求带回的版本、nonce 和错误，才是建立确认观察的关键输入。

沿着这条链，还能验证一个重要否定判断：cache 本身不会把 NACK 转换成全局业务回滚。你在这些核心函数里看到的是 watch、版本、订阅和通道机制；如果应用需要发布审批、分批推进、失败阈值和恢复策略，就必须在这些机制外设计明确状态。

## 14.5 用测试验证边界，而不只读名字

`snapshot.go` 的 `Consistent` 听起来覆盖很广，但实际只选择特定类型比较引用集合。打开同目录 `snapshot_test.go`，找到缺少 Endpoint、缺少 Route、额外资源或 scoped route 的用例，就能把抽象规则变成具体输入和预期错误。测试名称是导航，测试构造和断言才是证据。

如果你想研究“内容相同但版本改变时怎样响应”，读 `simple_test.go`；想研究按名称资源版本和显式删除，读 `delta_test.go`；想研究空列表与 `*` 的关系，读 `pkg/server/stream/v3/subscription_test.go` 及对应实现。测试通常比生产故障日志更容易隔离一个变量，适合在修改自己的控制平面前建立判断。

在固定源码快照内，可以用下面的搜索方式定位。命令作用是读取，不会修改仓库：

```sh
rg -n 'func .*SetSnapshot|func .*CreateWatch' pkg/cache/v3
rg -n 'ResponseNonce|OnStreamResponse|WithOrderedADS' pkg/server
rg -n 'func Test.*Consistent|func Test.*Snapshot' pkg/cache/v3
```

若要运行上游测试，先读根 `go.mod` 的 Go 版本、工具链和本地 replace，再在相应模块中选择包。上游根模块和 envoy 子模块是独立边界；在根目录执行 `go test ./...` 不会自然表达“连所有嵌套模块都测完了”。本书实验自己的 `go test ./...` 也只验证实验模块，不代表验证了所有上游代码。

## 14.6 文档与源码不一致时怎么办

版本固定后，优先使用该版本函数签名与测试来核实行为。例如该标签的 `docs/cache.md` 中仍有省略 context 的旧 `SetSnapshot` 片段，实际接口需要 `SetSnapshot(ctx, node, snapshot)`。如果照抄后报参数数量不对，应比较实际定义，不要为了迁就旧文档随意降级整个依赖树。

同样，注释可以说明设计意图，但实现中的条件决定该版本具体检查了什么。`Consistent` 的注释提到多类依赖，主循环却只选 Endpoint 与 Route 作为目标；这提醒我们报告时应说明实际覆盖范围。看到一个“支持所有 xDS 服务”的接口，也还要回到注册代码确认哪些服务真正暴露到网络。

读源码最终是建立可以反驳的判断：指出入口、条件、状态变更和可验证后果。若你能回答“我为什么认为这个版本会等待”“什么输入能证明我错了”，就已经从浏览代码进到理解实现，而不必把每一行生成代码都看完。

## 14.7 练习与参考答案

1. 从哪个文件验证 `SetSnapshot` 是否自动调用 `Consistent`？
2. 要确认 nonce 在哪一层生成，应该看 cache 还是流发送代码？
3. 复制文档的调用后缺少 context 参数，应先升级依赖吗？

参考答案：第一题看 `pkg/cache/v3/simple.go` 的实际函数体，再看 `snapshot.go` 中独立的检查方法；它们并未自动串联。第二题看 SotW `server.go` 的 `streamWrapper.send`，cache 的版本与流 nonce 分工不同。第三题先核对已固定版本的 API 定义和测试，修正调用；升级会改变学习基线，并不自动解决文档滞后。

## 本章源码入口

- [示例 main](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/internal/example/main/main.go)、[示例注册](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/internal/example/server.go)、[示例资源](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/internal/example/resource.go)。
- [cache 接口与响应](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/cache.go)、[SnapshotCache](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/simple.go)、[序列化缓存](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/cached_resource.go)。
- [组合 server](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/v3/server.go)、[SotW 请求循环](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/sotw/v3/xds.go)、[订阅状态](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/stream/v3/subscription.go)。
- [Snapshot 测试](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/snapshot_test.go)与[Delta 测试](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/delta_test.go)。
