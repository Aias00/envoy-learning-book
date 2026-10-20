# 第 11 章　SnapshotCache：保存配置，等待变化

资源构造完成之后，服务器还需要知道“哪个代理应该看到哪份配置”，并在配置变化时唤醒等待者。本章讲 SnapshotCache 的这两个职责。前置知识是[第 08 章](08-xds-protocol.md)的版本与订阅，以及[第 10 章](10-resource-model.md)的名称引用。目标是能够自己解释 `NewSnapshot`、`Consistent`、`SetSnapshot` 三个步骤，并知道它们分别没有保证什么。

可以先把 snapshot 想成一份已经编好的配置册：按资源类型分章节，每章按资源名存对象，附上版本。cache 像一个以代理分组键为索引的书架。服务器从书架取册子回复，业务翻译层负责写出新册子。cache 不会自己观察 Kubernetes，也不会根据用户的一个域名自动推导完整 Listener。

## 11.1 Node.ID 经过函数才成为缓存键

常用初始化方式如下，`cachev3` 是 `pkg/cache/v3` 的导入别名：

```go
snapCache := cachev3.NewSnapshotCache(true, cachev3.IDHash{}, nil)
```

第一个参数表示按 ADS 所需行为处理请求，不是“开启 gRPC 监听”；第二个参数规定怎样从 Node 算出查找键；第三个参数是可选日志器。默认 `IDHash` 直接返回 Node 的 `id`。假如 bootstrap 中 `node.id` 是 `edge-a`，应用通常用 `SetSnapshot(ctx, "edge-a", snapshot)` 发布。

关键是缓存实际使用 `NodeHash.ID(node)` 的输出，不是永远直接使用 Node.ID。生产环境可以按租户、区域或代理组做分组：多个不同 Node.ID 经过自定义哈希得到同一个组键，便共享一份配置。这可以减少重复构造与内存占用，但也意味着分组函数一旦过粗，就可能把不该共享的路由或 Secret 放到一起。

Node.ID 是客户端报告的字段，不能当作已经验证的身份。空 Node 在 `IDHash` 下得到空字符串；两个自称同一 ID 的客户端也会查到同一个键。身份认证、授权与分组策略需要在应用层建立关系。Node 的 metadata 也属于输入，不能因为写着 `tenant=finance` 就授予财务租户的配置读取权限。

## 11.2 三个动作要分开理解

典型发布片段如下。这里假定 `clusters`、`endpoints`、`listeners`、`routes` 都已构造好，类型为 `[]types.Resource`：

```go
snapshot, err := cachev3.NewSnapshot("release-17",
    map[resource.Type][]types.Resource{
        resource.ClusterType:  clusters,
        resource.EndpointType: endpoints,
        resource.ListenerType: listeners,
        resource.RouteType:    routes,
    },
)
if err != nil {
    return err
}
if err := snapshot.Consistent(); err != nil {
    return err
}
if err := snapCache.SetSnapshot(ctx, "edge-a", snapshot); err != nil {
    return err
}
```

`NewSnapshot` 把资源按类型和名称组织起来，拒绝未知的资源类型键；它并不会自动调用 `Consistent`。`Consistent` 按库支持的引用提取规则检查一部分集合关系。`SetSnapshot` 把新对象放进缓存，并尝试给已有 watch 发送更新；它同样不会替你调用 `Consistent`，也不会等待代理 ACK。

这里还有一个对错误处理很重要的细节：`v0.14.0` 的 `SetSnapshot` 先将快照写入内部 map，再通知等待者。如果后面的通知因为 context 取消等原因返回错误，不能据此断言“缓存没有改变”。调用者需要读取并确认缓存状态，按明确策略重试或重建期望状态，而不是把它当成一个失败就自动撤销的数据库事务。

## 11.3 Consistent 验证的是有限集合关系

考虑一个 EDS Cluster `orders`，它引用名为 `orders` 的地址集合。若快照里只有 `payments` 的 ClusterLoadAssignment，检查应失败。若有 `orders`，另外还附带一个完全未被引用的 `unused`，对这份全量 snapshot 的检查也会失败：它比较的是引用集合与实际集合相等，不只是“缺的都补上了”。

对 RDS 也类似。Listener 内 HCM 引用 `shop-routes`，快照需要对应 RouteConfiguration；有两个引用时，两者都必须存在。源码还会从 ScopedRouteConfiguration 以及内嵌的 scoped route 列表提取 RDS 名称。要注意，在该版本的 `Consistent()` 主循环中，实际选为校验目标的响应类型是 Endpoint 和 Route；不要只读注释就把它想成覆盖所有资源类型的通用图验证器。

哪些问题不会因此得到全面验证？Route 指向一个不存在的业务 Cluster，SDS 引用缺少证书，Filter 的 Any 内容不合适，端口已被其他程序占用，以及地址根本无法连通，都不能仅凭 `Consistent()==nil` 排除。特别是 Route→Cluster 引用，源码明确没有把它纳入该引用检查，因为 Cluster 当前按批量获取。这是一项实现范围，不是“路由引用不存在的 Cluster 没关系”。

因此合理的分层是：先检查输入与重复名，再做具体资源的 schema/语义校验，接着做库的集合检查与应用自己的引用检查，最后由代理接受并用业务请求验证。某个资源的 `ValidateAll()` 即使通过，也不等于跨资源关系和实际网络已经正确。

## 11.4 watch 是等待条件，不是忙循环

Envoy 发来“我已经有 RDS 版本 release-17”时，如果缓存也是这个版本，并且没有尚未返回的新订阅资源，服务器不需要立即发一份相同内容。cache 保存一个 watch：记录请求、订阅状态和响应通道，等以后条件发生变化再唤醒。

应用调用 `SetSnapshot` 写入 `release-18` 后，cache 会比较已有 watch 请求中的版本与新版本，将符合条件的响应送到通道，服务器再序列化并发到 gRPC 流。响应发出后，客户端通过下一条请求继续对话，服务器相应创建或替换 watch。流关闭时要取消旧 watch，避免已经离开的客户端一直占着内存。

实际代码还处理“版本相同但新订阅资源尚未返回”的情况，所以不能把 watch 机制概括成单一 `oldVersion != newVersion` 判断。在 ADS 模式下，cache 还会对有名称的请求检查资源集合是否覆盖快照所需名称。如果你做了一份包含大量互不相关路由的快照，而某代理只请求其中一小部分，可能看到它一直等不到你预想的回答。分组与快照边界需要和订阅模型一致。

## 11.5 版本和不可变约定相互依赖

SotW 依赖版本变化发现更新。若你修改后端内容，却继续使用相同类型版本，已有客户端可能继续等待。反过来，每次无意义地换版本，又会使没有内容变化的资源反复传输。一个良好策略是让版本可靠表示该类型已发布内容，使用单调发布号或稳定内容摘要，避免重启和不同副本复用同版本却对应不同内容。

发布后的 snapshot 及其 Protobuf 对象应视为不可变。`NewSnapshot` 建立索引，不是对每个对象做深拷贝；把指针塞进快照后继续改字段，可能导致数据竞争，也可能让内容改变而版本仍然不变。对于 Delta，`ConstructVersionMap` 还会基于确定性序列化的字节计算资源摘要，并缓存结果；原地修改更容易造成摘要和对象内容脱节。

更清晰的做法是在新输入到达后构造新的资源对象和新快照，再一次替换缓存条目。若复用旧对象，必须有严格的只读约定；确实需要修改旧 Protobuf 时，先通过 `proto.Clone` 等方式创建独立对象。不要以为 cache 内部有锁，就能保护应用在锁外修改消息字段。

## 11.6 何时考虑其他 cache

SnapshotCache 适合把一组代理的配置作为整体生成。若 EDS 有海量地址、变化频繁，而其他类型变化很少，可研究 LinearCache。它维护单一 type URL 的资源集合与版本，适合按名称更新，不会自动承担其他资源类型的依赖检查。MuxCache 再按分类函数把请求送到不同 cache，例如 EDS 去 LinearCache，LDS/RDS 去 SnapshotCache。

组合提高了灵活性，也使跨缓存的一致性更依赖应用发布策略。把同一个资源拆进两套缓存、分类漏掉某个类型或不同副本状态不同，都可能造成困难的间歇故障。入门实验先保持一种缓存模型，理解每次变化的路径，再用实际资源规模和性能数据决定是否拆分。

## 11.7 练习与参考答案

1. Node.ID 为 `edge-a`，自定义 NodeHash 返回 `team-red`，`SetSnapshot` 应用哪个键？
2. 新端点已经放入新对象，但快照版本完全没变，为什么已有 SotW 客户端可能看不到变化？
3. `Consistent()` 通过，Route 指向 `missing-cluster`，是否矛盾？

参考答案：第一题用 `team-red`，缓存查找以哈希结果为准。第二题版本是重要唤醒条件，不能让相同版本代表不同内容。第三题不矛盾，这条引用不在该版本的一致性检查范围内，应用仍应发现并拒绝它。可再思考：`SetSnapshot` 返回错误后，为什么必须确认当前缓存，而不是直接记为“未发布”？原因在于它先存储再通知。

## 本章源码

- [Snapshot 的构造、检查和 Delta 版本图](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/snapshot.go)。
- [SnapshotCache、SetSnapshot 与 CreateWatch](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/simple.go)。
- [NodeHash 与 IDHash](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/status.go)、[按名建索引](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/resources.go)。
- [一致性检查测试](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/snapshot_test.go)、[LinearCache](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/linear.go)与[MuxCache](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/mux.go)。
