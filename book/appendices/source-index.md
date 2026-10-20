# 附录 C　按问题查源码

不要把读源码理解为从目录第一行逐个打开文件。带着一个可观察的问题，从入口追到状态变化，再读附近测试，通常更容易建立模型。固定 tag 与 commit 见[版本说明](versions.md)。

## go-control-plane v0.14.0

以下链接都指向同一固定版本。

| 我的问题 | 入口 | 阅读任务 |
| --- | --- | --- |
| 库到底负责什么 | [README](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/README.md) | 标出 API server/cache 与业务翻译的责任边界 |
| 根与子模块依赖怎么选 | [go.mod](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/go.mod) | 区分 require 和上游本地 replace |
| NewSnapshot 构造了什么 | [snapshot.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/snapshot.go) | 比较 NewSnapshot、Consistent、GetVersion |
| 引用集合怎么提取 | [resource.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/resource.go) | 找 EDS 与 RDS 引用，列出未检查关系 |
| SetSnapshot 是否自动校验 | [simple.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/simple.go) | 看写缓存与通知 watch 的顺序、错误路径 |
| Node.ID 怎样变成缓存键 | [status.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/status.go) | 找 IDHash，再追 simple.go 调用点 |
| 每类型资源和版本怎么存 | [resources.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/cache/v3/resources.go) | 看 NewResources 和 Items，理解名字索引 |
| server.NewServer 怎么组合协议 | [server/v3/server.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/v3/server.go) | 找 SotW/Delta 组合、Callbacks 接口 |
| SotW 请求与 nonce 怎么处理 | [sotw/v3/xds.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/sotw/v3/xds.go) | 从 DiscoveryRequest 追到 watch 变更 |
| 响应何时发出 | [sotw/v3/server.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/sotw/v3/server.go) | 区分准备响应回调与实际网络 Send |
| Delta 与 SotW 有何不同 | [delta/v3/server.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/pkg/server/delta/v3/server.go) | 对照新增、删除和资源级版本状态 |
| ADS gRPC 注册是什么 | [ads.pb.go](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/envoy/service/discovery/v3/ads.pb.go) | 找 RegisterAggregatedDiscoveryServiceServer |

建议先把实验二的 `buildSnapshot` 一类资源构造函数与这些接口对上，再挑一个测试文件复现自己的问题。不要复制缓存内部私有类型来扩展业务，它们的实现细节不构成稳定产品接口。

## Envoy Gateway v1.9.2

| 我的问题 | 固定版本入口 | 阅读任务 |
| --- | --- | --- |
| 支持哪些版本 | [matrix.md](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/news/releases/matrix.md) | 记录 EG、Gateway API、K8s、Envoy 四维关系 |
| 程序怎么启动 | [cmd/envoy-gateway](https://github.com/envoyproxy/gateway/tree/v1.9.2/cmd/envoy-gateway) | 追运行配置与 server 组装 |
| Kubernetes 对象从哪里来 | [internal/provider/kubernetes](https://github.com/envoyproxy/gateway/tree/v1.9.2/internal/provider/kubernetes) | 找 watch、对象选择与状态更新 |
| HTTPRoute 如何翻译 | [internal/gatewayapi](https://github.com/envoyproxy/gateway/tree/v1.9.2/internal/gatewayapi) | 找 parent/引用校验与规则处理 |
| 中间表示有哪些字段 | [internal/ir](https://github.com/envoyproxy/gateway/tree/v1.9.2/internal/ir) | 把外部 API 与 IR 做一对多映射 |
| IR 如何变成 Envoy 资源 | [internal/xds/translator](https://github.com/envoyproxy/gateway/tree/v1.9.2/internal/xds/translator) | 追 Listener、Route、Cluster 和 EDS 构造 |
| xDS 如何运行 | [internal/xds/server](https://github.com/envoyproxy/gateway/tree/v1.9.2/internal/xds/server) | 看 cache/server、节点与资源发布 |
| 谁创建数据平面资源 | [internal/infrastructure](https://github.com/envoyproxy/gateway/tree/v1.9.2/internal/infrastructure) | 区分配置翻译与 Deployment/Service 管理 |
| 策略字段是否真实存在 | [api/v1alpha1](https://github.com/envoyproxy/gateway/tree/v1.9.2/api/v1alpha1) | 查字段、校验与生成 CRD，别只看旧教程 |
| 当前输入应生成什么结果 | [gatewayapi/testdata](https://github.com/envoyproxy/gateway/tree/v1.9.2/internal/gatewayapi/testdata) | 选最小 fixture，比较输入、IR 与状态 |
| 安装资源怎么打包 | [charts/gateway-helm](https://github.com/envoyproxy/gateway/tree/v1.9.2/charts/gateway-helm) | 核对 CRD、RBAC、镜像与值配置 |

这些目录展示架构边界，不表示请求会逐个经过这些 Go 包。业务数据由生成配置后的 Envoy 处理；这些包主要参与配置与基础设施管理。

## 一页源码笔记模板

```text
问题：我在实验里观察到什么？
版本：tag / commit / 工具版本
入口：包、函数、输入对象
转换：调用哪些函数，改变什么状态
输出：资源、事件、响应或错误
边界：源码保证什么，什么仍要由调用者负责
证据：对应测试/命令/真实请求
未解问题：下一步需要沿哪条调用关系继续查
```

写出这七项，通常比粘贴大段代码更能帮助下一次阅读。
